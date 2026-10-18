// Package hostnet detects when the host's default route is captured by a TUN
// interface so a non-TUN cx instance can bind outbound sockets to the physical
// interface instead of looping back into that TUN.
package hostnet

import (
	"sort"
	"strings"
)

// MainTable is the Linux routing table consulted for the physical default
// route. It also matches the implicit table used by the other platforms.
const MainTable = 254

const anyTable = -1

// Route describes one default route. Non-default routes are not needed by the
// detection logic and are normally discarded by the platform readers.
type Route struct {
	Family int
	Table  int
	Device string
	Metric int
	// Tun marks a device that the platform identified as a TUN-like link even
	// when its name is not in the usual naming scheme.
	Tun bool
}

// Rule describes an IP policy-routing rule relevant to default-route lookup.
// Priority follows the kernel convention: lower values win.
type Rule struct {
	Family   int
	Priority int
	Table    int
	MatchAll bool
}

// Detection is the result of a successful host TUN detection.
type Detection struct {
	Tun      string
	Physical string
}

// Detect inspects the host routing table and returns a detection only when the
// effective default route uses a TUN-like device and a physical default route
// is still available. Unsupported platforms return false without an error.
func Detect() (Detection, bool) {
	routes, rules, ok := readSystemRoutes()
	if !ok {
		return Detection{}, false
	}
	return Decide(routes, rules)
}

// Decide applies the detection decision to route and policy-rule snapshots.
func Decide(routes []Route, rules []Rule) (Detection, bool) {
	effective := effectiveTables(routes, rules)
	for _, family := range []int{4, 6} {
		current := bestDefault(routes, family, effective[family], false)
		if current == nil || !isTunRoute(*current) {
			continue
		}
		physical := bestDefault(routes, family, MainTable, true)
		if physical == nil {
			physical = bestDefault(routes, family, anyTable, true)
		}
		if physical == nil {
			physical = bestDefault(routes, 0, anyTable, true)
		}
		if physical == nil || physical.Device == "" {
			continue
		}
		return Detection{Tun: current.Device, Physical: physical.Device}, true
	}
	return Detection{}, false
}

func effectiveTables(routes []Route, rules []Rule) map[int]int {
	tables := map[int]int{4: MainTable, 6: MainTable}
	for _, family := range []int{4, 6} {
		type candidate struct {
			priority int
			table    int
		}
		candidates := []candidate{{priority: int(^uint(0) >> 1), table: MainTable}}
		for _, rule := range rules {
			if !rule.MatchAll || rule.Table <= 0 {
				continue
			}
			if rule.Family != 0 && rule.Family != family {
				continue
			}
			candidates = append(candidates, candidate{priority: rule.Priority, table: rule.Table})
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			return candidates[i].priority < candidates[j].priority
		})
		for _, candidate := range candidates {
			if hasDefaultRoute(routes, family, candidate.table) {
				tables[family] = candidate.table
				break
			}
		}
	}
	return tables
}

func hasDefaultRoute(routes []Route, family, table int) bool {
	for _, route := range routes {
		if route.Family == family && route.Table == table && route.Device != "" {
			return true
		}
	}
	return false
}

func bestDefault(routes []Route, family, table int, physicalOnly bool) *Route {
	var best *Route
	for i := range routes {
		route := &routes[i]
		if route.Device == "" {
			continue
		}
		if family != 0 && route.Family != family {
			continue
		}
		if table != anyTable && route.Table != table {
			continue
		}
		if physicalOnly && isTunRoute(*route) {
			continue
		}
		if best == nil || route.Metric < best.Metric {
			best = route
		}
	}
	return best
}

func isTunRoute(route Route) bool {
	return route.Tun || IsTunName(route.Device)
}

// IsTunName recognizes the common TUN, WireGuard, and Tailscale device names
// used by mihomo, sing-tun, and the optional outbounds.
func IsTunName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	if name == "meta" {
		return true
	}
	for _, prefix := range []string{"utun", "tun", "tailscale", "wg"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	if strings.HasSuffix(name, "tun") || strings.HasSuffix(name, "tap") {
		return true
	}
	for _, word := range []string{"wintun", "wireguard"} {
		if strings.Contains(name, word) {
			return true
		}
	}
	return false
}
