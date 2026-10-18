//go:build linux

package hostnet

import (
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

type publicRouteTarget struct {
	family  uint8
	address netip.Addr
}

var publicRouteTargets = []publicRouteTarget{
	{family: unix.AF_INET, address: netip.MustParseAddr("1.1.1.1")},
	{family: unix.AF_INET6, address: netip.MustParseAddr("2606:4700:4700::1111")},
}

type routeNetlink struct {
	fd  int
	seq uint32
}

func readSystemRoutes() ([]Route, []Rule, bool) {
	if routes, rules, ok := readNetlinkRoutes(); ok && len(routes) > 0 {
		return routes, rules, true
	}
	routes, ok := readProcRoutes()
	if !ok {
		return nil, nil, false
	}
	markLinuxTunDevices(routes)
	return routes, nil, len(routes) > 0
}

func readNetlinkRoutes() ([]Route, []Rule, bool) {
	conn, err := openRouteNetlink()
	if err != nil {
		return nil, nil, false
	}
	defer conn.Close()

	var routes []Route
	var rules []Rule
	for _, target := range publicRouteTargets {
		lookedUp, err := conn.request(target.family, target.address, 0, false)
		if err != nil || len(lookedUp) == 0 {
			continue
		}
		route, ok := routeFromNetlink(lookedUp[0])
		if !ok {
			continue
		}
		routes = append(routes, route)
		// The lookup result is the kernel's effective route, including any
		// policy table selected by ip rules. Make sure Decide uses it before
		// consulting the main table.
		rules = append(rules, Rule{Family: route.Family, Priority: 0, Table: route.Table, MatchAll: true})
	}

	for _, family := range []uint8{unix.AF_INET, unix.AF_INET6} {
		dumped, err := conn.request(family, netip.Addr{}, MainTable, true)
		if err != nil {
			continue
		}
		for _, candidate := range dumped {
			if candidate.DstLength != 0 || candidate.Table != MainTable || candidate.IfIndex == 0 {
				continue
			}
			if route, ok := routeFromNetlink(candidate); ok {
				routes = append(routes, route)
			}
		}
	}

	if len(routes) == 0 {
		return nil, nil, false
	}
	markLinuxTunDevices(routes)
	return routes, rules, true
}

func routeFromNetlink(message netlinkRoute) (Route, bool) {
	iface, err := net.InterfaceByIndex(int(message.IfIndex))
	if err != nil || iface.Name == "" {
		return Route{}, false
	}
	return Route{
		Family: message.Family,
		Table:  message.Table,
		Device: iface.Name,
		Metric: int(message.Metric),
	}, true
}

func openRouteNetlink() (*routeNetlink, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_ROUTE)
	if err != nil {
		return nil, err
	}
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	timeout := unix.Timeval{Sec: 2}
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &timeout); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	return &routeNetlink{fd: fd}, nil
}

func (conn *routeNetlink) Close() error {
	return unix.Close(conn.fd)
}

func (conn *routeNetlink) request(family uint8, destination netip.Addr, table int, dump bool) ([]netlinkRoute, error) {
	conn.seq++
	request := marshalNetlinkRouteRequest(conn.seq, family, destination, table, dump)
	if err := unix.Sendto(conn.fd, request, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return nil, err
	}

	var routes []netlinkRoute
	buffer := make([]byte, 64*1024)
	for {
		n, _, err := unix.Recvfrom(conn.fd, buffer, 0)
		if err != nil {
			return routes, fmt.Errorf("receive netlink route response: %w", err)
		}
		data := buffer[:n]
		for len(data) >= netlinkHeaderLength {
			length := int(binary.NativeEndian.Uint32(data[0:4]))
			if length < netlinkHeaderLength || length > len(data) {
				return routes, fmt.Errorf("invalid netlink response length %d", length)
			}
			messageType := binary.NativeEndian.Uint16(data[4:6])
			sequence := binary.NativeEndian.Uint32(data[8:12])
			if sequence != conn.seq {
				data = data[minInt(alignNetlink(length), len(data)):]
				continue
			}
			switch messageType {
			case netlinkMessageError:
				if length < netlinkHeaderLength+4 {
					return routes, fmt.Errorf("short netlink error response")
				}
				errno := int32(binary.NativeEndian.Uint32(data[netlinkHeaderLength : netlinkHeaderLength+4]))
				if errno != 0 {
					return routes, unix.Errno(-errno)
				}
			case netlinkMessageDone:
				return routes, nil
			default:
				parsed, err := parseNetlinkRouteMessages(data[:length])
				if err != nil {
					return routes, err
				}
				routes = append(routes, parsed...)
			}
			data = data[minInt(alignNetlink(length), len(data)):]
		}
		if !dump {
			return routes, nil
		}
	}
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func markLinuxTunDevices(routes []Route) {
	for i := range routes {
		if routes[i].Tun {
			continue
		}
		if isLinuxTunDevice(routes[i].Device) {
			routes[i].Tun = true
		}
	}
}

func isLinuxTunDevice(device string) bool {
	if device == "" {
		return false
	}
	if _, err := os.Stat("/sys/class/net/" + device + "/tun_flags"); err == nil {
		return true
	}
	if IsTunName(device) {
		return true
	}
	// ARPHRD_NONE (65534) is used by tun/tap and WireGuard links.
	data, err := os.ReadFile("/sys/class/net/" + device + "/type")
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == "65534"
}

func readProcRoutes() ([]Route, bool) {
	var routes []Route
	ok := false
	if data, err := os.ReadFile("/proc/net/route"); err == nil {
		routes = append(routes, parseProcNetRoute(string(data))...)
		ok = true
	}
	if data, err := os.ReadFile("/proc/net/ipv6_route"); err == nil {
		routes = append(routes, parseProcNetIPv6Route(string(data))...)
		ok = true
	}
	return routes, ok
}
