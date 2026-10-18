package hostnet

import "testing"

const procNetRouteFixture = `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
eth0	00000000	0102A8C0	0003	0	0	100	00000000	0	0	0
Meta	00000000	00000000	0001	0	0	0	00000000	0	0	0
eth0	0001A8C0	00000000	0001	0	0	100	0000FFFF	0	0	0
`

const procNetIPv6RouteFixture = `00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000000 00000400 00000000 00000000 00000003 eth0
fe800000000000000000000000000000 40 00000000000000000000000000000000 00 00000000000000000000000000000000 00000400 00000000 00000000 00000001 eth0
00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000000 00000000 00000000 00000000 00000003 Meta
`

const netstatFixture = `Routing tables

Internet:
Destination        Gateway            Flags        Netif Expire
default            utun3              UGScg        utun3
default            192.168.1.1        UGScg          en0
127.0.0.1          127.0.0.1          UH             lo0
`

const windowsRoutePrint4Fixture = `
===========================================================================
IPv4 Route Table
===========================================================================
Active Routes:
Network Destination        Netmask          Gateway       Interface  Metric
          0.0.0.0          0.0.0.0      192.168.1.1     192.168.1.20     25
        192.168.1.0    255.255.255.0         On-link      192.168.1.20    281
===========================================================================
Persistent Routes:
  None
`

const windowsRoutePrint6Fixture = `
===========================================================================
IPv6 Route Table
===========================================================================
Active Routes:
 If Metric Network Destination      Gateway
 12     25 ::/0                     fe80::1
  1    281 fe80::/64                On-link
===========================================================================
Persistent Routes:
  None
`

func TestParseProcNetRoute(t *testing.T) {
	routes := parseProcNetRoute(procNetRouteFixture)
	if len(routes) != 2 {
		t.Fatalf("routes = %#v, want 2 default routes", routes)
	}
	if routes[0].Device != "eth0" || routes[0].Metric != 100 || routes[0].Table != MainTable {
		t.Errorf("first route = %#v", routes[0])
	}
	if routes[1].Device != "Meta" || routes[1].Metric != 0 {
		t.Errorf("second route = %#v", routes[1])
	}
}

func TestParseProcNetIPv6Route(t *testing.T) {
	routes := parseProcNetIPv6Route(procNetIPv6RouteFixture)
	if len(routes) != 2 {
		t.Fatalf("routes = %#v, want 2 default routes", routes)
	}
	if routes[0].Device != "eth0" || routes[0].Metric != 1024 {
		t.Errorf("first route = %#v", routes[0])
	}
	if routes[1].Device != "Meta" || routes[1].Metric != 0 {
		t.Errorf("second route = %#v", routes[1])
	}
}

func TestParseNetstatRoutes(t *testing.T) {
	routes := parseNetstatRoutes(netstatFixture, 4)
	if len(routes) != 2 {
		t.Fatalf("routes = %#v, want 2 default routes", routes)
	}
	if routes[0].Device != "utun3" || routes[0].Metric != 0 {
		t.Errorf("first route = %#v", routes[0])
	}
	if routes[1].Device != "en0" || routes[1].Metric != 1 {
		t.Errorf("second route = %#v", routes[1])
	}
}

func TestParseWindowsRoutes(t *testing.T) {
	v4 := parseWindowsIPv4Routes(windowsRoutePrint4Fixture)
	if len(v4) != 1 || v4[0].Device != "192.168.1.20" || v4[0].Metric != 25 {
		t.Errorf("IPv4 routes = %#v", v4)
	}
	v6 := parseWindowsIPv6Routes(windowsRoutePrint6Fixture)
	if len(v6) != 1 || v6[0].Device != "12" || v6[0].Metric != 25 {
		t.Errorf("IPv6 routes = %#v", v6)
	}
}

func TestDecideHostTunUsesPhysicalDefault(t *testing.T) {
	routes := append(parseProcNetRoute(procNetRouteFixture), parseProcNetIPv6Route(procNetIPv6RouteFixture)...)
	detection, ok := Decide(routes, nil)
	if !ok {
		t.Fatal("Decide did not detect the host TUN")
	}
	if detection.Tun != "Meta" || detection.Physical != "eth0" {
		t.Errorf("detection = %#v, want Meta -> eth0", detection)
	}
}

func TestDecidePolicyRouting(t *testing.T) {
	routes := []Route{
		{Family: 4, Table: MainTable, Device: "eth0", Metric: 100},
		{Family: 4, Table: 2022, Device: "Meta", Metric: 0},
	}
	rules := []Rule{
		{Family: 4, Priority: 9000, Table: 2022, MatchAll: true},
		{Family: 4, Priority: 32766, Table: MainTable, MatchAll: true},
	}
	detection, ok := Decide(routes, rules)
	if !ok || detection.Tun != "Meta" || detection.Physical != "eth0" {
		t.Fatalf("detection = %#v, %v; want policy-routed Meta -> eth0", detection, ok)
	}
}

func TestDecideSkipsPolicyTablesWithoutDefault(t *testing.T) {
	routes := []Route{
		{Family: 4, Table: MainTable, Device: "eth0", Metric: 100},
		{Family: 4, Table: 2022, Device: "Meta", Metric: 0},
	}
	rules := []Rule{
		{Family: 4, Priority: 0, Table: 255, MatchAll: true},
		{Family: 4, Priority: 9000, Table: 2022, MatchAll: true},
		{Family: 4, Priority: 32766, Table: MainTable, MatchAll: true},
	}
	detection, ok := Decide(routes, rules)
	if !ok || detection.Tun != "Meta" || detection.Physical != "eth0" {
		t.Fatalf("detection = %#v, %v; want Meta -> eth0", detection, ok)
	}
}

func TestDecideNoTun(t *testing.T) {
	routes := []Route{{Family: 4, Table: MainTable, Device: "eth0", Metric: 100}}
	if detection, ok := Decide(routes, nil); ok {
		t.Fatalf("Decide = %#v, true; want no detection", detection)
	}
}

func TestDecideTunWithoutPhysicalDefault(t *testing.T) {
	routes := []Route{{Family: 4, Table: MainTable, Device: "Meta", Metric: 0}}
	if detection, ok := Decide(routes, nil); ok {
		t.Fatalf("Decide = %#v, true; want no physical fallback", detection)
	}
}

func TestDecideTunDeviceType(t *testing.T) {
	routes := []Route{
		{Family: 4, Table: MainTable, Device: "vpn0", Metric: 0, Tun: true},
		{Family: 4, Table: MainTable, Device: "eth0", Metric: 100},
	}
	detection, ok := Decide(routes, nil)
	if !ok || detection.Tun != "vpn0" || detection.Physical != "eth0" {
		t.Fatalf("detection = %#v, %v; want type-marked vpn0 -> eth0", detection, ok)
	}
}
