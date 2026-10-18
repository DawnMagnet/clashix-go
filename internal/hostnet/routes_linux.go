//go:build linux

package hostnet

import (
	"net"
	"os"
	"strings"

	"github.com/jsimonetti/rtnetlink"
)

func readSystemRoutes() ([]Route, []Rule, bool) {
	if routes, rules, ok := readNetlinkRoutes(); ok && len(routes) > 0 {
		markLinuxTunDevices(routes)
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
	conn, err := rtnetlink.Dial(nil)
	if err != nil {
		return nil, nil, false
	}
	defer conn.Close()

	messages, err := conn.Route.List()
	if err != nil {
		return nil, nil, false
	}
	routes := make([]Route, 0, len(messages))
	for _, message := range messages {
		family := addressFamily(message.Family)
		if family == 0 {
			continue
		}
		// Only unicast default routes matter. RTN_UNICAST is 1 on Linux.
		if message.DstLength != 0 || message.Type != 1 {
			continue
		}
		table := int(message.Table)
		if message.Attributes.Table != 0 {
			table = int(message.Attributes.Table)
		}
		if table == 0 {
			table = MainTable
		}
		if message.Attributes.OutIface == 0 {
			continue
		}
		iface, err := net.InterfaceByIndex(int(message.Attributes.OutIface))
		if err != nil {
			continue
		}
		routes = append(routes, Route{
			Family: family,
			Table:  table,
			Device: iface.Name,
			Metric: int(message.Attributes.Priority),
		})
	}

	ruleMessages, err := conn.Rule.List()
	if err != nil {
		return routes, nil, true
	}
	rules := make([]Rule, 0, len(ruleMessages))
	for _, message := range ruleMessages {
		rule := Rule{
			Family:   addressFamily(message.Family),
			Priority: 32767,
			Table:    int(message.Table),
		}
		if attributes := message.Attributes; attributes != nil {
			if attributes.Priority != nil {
				rule.Priority = int(*attributes.Priority)
			}
			if attributes.Table != nil {
				rule.Table = int(*attributes.Table)
			}
			rule.MatchAll = ruleMatchesAll(attributes)
		} else {
			rule.MatchAll = true
		}
		rules = append(rules, rule)
	}
	return routes, rules, true
}

func addressFamily(value uint8) int {
	switch value {
	case 2: // AF_INET
		return 4
	case 10: // AF_INET6
		return 6
	default:
		return 0
	}
}

func ruleMatchesAll(attributes *rtnetlink.RuleAttributes) bool {
	if attributes == nil {
		return true
	}
	if attributes.Src != nil || attributes.Dst != nil ||
		attributes.FwMark != nil || attributes.FwMask != nil ||
		attributes.UIDRange != nil || attributes.SPortRange != nil ||
		attributes.DPortRange != nil || attributes.OIFName != nil {
		return false
	}
	// sing-tun routes all non-loopback traffic through its table. Treat the
	// loopback interface restriction (usually an inverted `iif lo`) as an
	// all-traffic rule; more specific interface rules are ignored.
	if attributes.IIFName != nil && *attributes.IIFName != "lo" {
		return false
	}
	return true
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

func markLinuxTunDevices(routes []Route) {
	for i := range routes {
		if routes[i].Tun {
			continue
		}
		if IsTunName(routes[i].Device) {
			routes[i].Tun = true
			continue
		}
		// ARPHRD_NONE (65534) is used by tun/tap and WireGuard links.
		data, err := os.ReadFile("/sys/class/net/" + routes[i].Device + "/type")
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(data)) == "65534" {
			routes[i].Tun = true
		}
	}
}
