package hostnet

import (
	"strconv"
	"strings"
)

// parseProcNetRoute parses the IPv4 main routing table from /proc/net/route.
// Columns are: Iface Destination Gateway Flags RefCnt Use Metric Mask MTU
// Window IRTT. Destinations and masks are little-endian hexadecimal.
func parseProcNetRoute(data string) []Route {
	var routes []Route
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 || fields[0] == "Iface" {
			continue
		}
		if fields[1] != "00000000" || fields[7] != "00000000" {
			continue
		}
		metric, err := strconv.Atoi(fields[6])
		if err != nil {
			continue
		}
		routes = append(routes, Route{
			Family: 4,
			Table:  MainTable,
			Device: fields[0],
			Metric: metric,
		})
	}
	return routes
}

// parseProcNetIPv6Route parses default routes from /proc/net/ipv6_route. The
// kernel exposes destinations, metrics, flags, and the device as hexadecimal
// fields; this file does not identify the policy table.
func parseProcNetIPv6Route(data string) []Route {
	var routes []Route
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 || fields[1] != "00" {
			continue
		}
		if strings.Trim(fields[0], "0") != "" {
			continue
		}
		metric, err := strconv.ParseUint(fields[5], 16, 64)
		if err != nil {
			continue
		}
		routes = append(routes, Route{
			Family: 6,
			Table:  MainTable,
			Device: fields[9],
			Metric: int(metric),
		})
	}
	return routes
}

// parseNetstatRoutes parses macOS `netstat -rn -f inet` or `-f inet6` output.
// Default routes are listed in route preference order, which is used as a
// stand-in for the metric.
func parseNetstatRoutes(data string, family int) []Route {
	var routes []Route
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		if fields[0] != "default" && fields[0] != "::/0" && fields[0] != "0.0.0.0/0" {
			continue
		}
		device := fields[3]
		if device == "" || strings.HasPrefix(device, "link#") {
			continue
		}
		routes = append(routes, Route{
			Family: family,
			Table:  MainTable,
			Device: device,
			Metric: len(routes),
		})
	}
	return routes
}

// parseWindowsIPv4Routes parses the active IPv4 default routes from
// `route print -4`. Device is the interface address; the Windows reader maps
// it to an interface name.
func parseWindowsIPv4Routes(data string) []Route {
	var routes []Route
	active := false
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if strings.EqualFold(line, "Active Routes:") {
			active = true
			continue
		}
		if !active {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "persistent routes:") {
			break
		}
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "0.0.0.0" || fields[1] != "0.0.0.0" {
			continue
		}
		metric, err := strconv.Atoi(fields[4])
		if err != nil {
			continue
		}
		routes = append(routes, Route{
			Family: 4,
			Table:  MainTable,
			Device: fields[3],
			Metric: metric,
		})
	}
	return routes
}

// parseWindowsIPv6Routes parses the active IPv6 default routes from
// `route print -6`. Device is the numeric interface index; the Windows reader
// maps it to an interface name.
func parseWindowsIPv6Routes(data string) []Route {
	var routes []Route
	active := false
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if strings.EqualFold(line, "Active Routes:") {
			active = true
			continue
		}
		if !active {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "persistent routes:") {
			break
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[2] != "::/0" {
			continue
		}
		metric, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		routes = append(routes, Route{
			Family: 6,
			Table:  MainTable,
			Device: fields[0],
			Metric: metric,
			Tun:    false,
		})
	}
	return routes
}
