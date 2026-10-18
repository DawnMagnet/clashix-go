//go:build windows

package hostnet

import (
	"net"
	"os/exec"
	"strconv"
	"strings"
)

func readSystemRoutes() ([]Route, []Rule, bool) {
	var routes []Route
	ok := false
	if output, err := exec.Command("route", "print", "-4").Output(); err == nil {
		ok = true
		parsed := parseWindowsIPv4Routes(string(output))
		names := windowsAddressNames()
		for _, route := range parsed {
			if name, found := names[route.Device]; found {
				route.Device = name
			}
			routes = append(routes, route)
		}
	}
	if output, err := exec.Command("route", "print", "-6").Output(); err == nil {
		ok = true
		parsed := parseWindowsIPv6Routes(string(output))
		for _, route := range parsed {
			index, err := strconv.Atoi(route.Device)
			if err != nil {
				continue
			}
			iface, err := net.InterfaceByIndex(index)
			if err != nil {
				continue
			}
			route.Device = iface.Name
			routes = append(routes, route)
		}
	}
	markWindowsTunDevices(routes)
	return routes, nil, ok
}

func windowsAddressNames() map[string]string {
	names := make(map[string]string)
	interfaces, err := net.Interfaces()
	if err != nil {
		return names
	}
	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err != nil {
				continue
			}
			names[ip.String()] = iface.Name
		}
	}
	return names
}

func markWindowsTunDevices(routes []Route) {
	for i := range routes {
		if IsTunName(routes[i].Device) || strings.Contains(strings.ToLower(routes[i].Device), "wintun") {
			routes[i].Tun = true
		}
	}
}
