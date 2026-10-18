//go:build darwin

package hostnet

import (
	"os/exec"
	"strings"
)

func readSystemRoutes() ([]Route, []Rule, bool) {
	var routes []Route
	ok := false
	for _, family := range []struct {
		name string
		fam  int
	}{{"inet", 4}, {"inet6", 6}} {
		output, err := exec.Command("netstat", "-rn", "-f", family.name).Output()
		if err != nil {
			continue
		}
		ok = true
		routes = append(routes, parseNetstatRoutes(string(output), family.fam)...)
	}
	markDarwinTunDevices(routes)
	return routes, nil, ok
}

func markDarwinTunDevices(routes []Route) {
	for i := range routes {
		if IsTunName(routes[i].Device) || strings.HasPrefix(strings.ToLower(routes[i].Device), "utun") {
			routes[i].Tun = true
		}
	}
}
