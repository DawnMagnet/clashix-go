package hostnet

import (
	"encoding/binary"
	"net/netip"
	"testing"
)

func TestParseNetlinkRouteMessages(t *testing.T) {
	frame := testNetlinkRouteFrame(2, 32, 2022, 7, 50, 2022)
	frame = append(frame, testNetlinkRouteFrame(10, 128, 254, 3, 1024, 0)...)

	routes, err := parseNetlinkRouteMessages(frame)
	if err != nil {
		t.Fatalf("parseNetlinkRouteMessages: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("routes = %#v, want 2", routes)
	}
	first := routes[0]
	if first.Family != 4 || first.DstLength != 32 || first.Table != 2022 || first.IfIndex != 7 || first.Metric != 50 {
		t.Errorf("first route = %#v", first)
	}
	second := routes[1]
	if second.Family != 6 || second.DstLength != 128 || second.Table != 254 || second.IfIndex != 3 || second.Metric != 1024 {
		t.Errorf("second route = %#v", second)
	}
}

func TestParseNetlinkRouteRejectsShortMessage(t *testing.T) {
	if _, err := parseNetlinkRoute(make([]byte, 8)); err == nil {
		t.Fatal("parseNetlinkRoute accepted a short message")
	}
	shortFrame := make([]byte, netlinkHeaderLength)
	binary.NativeEndian.PutUint32(shortFrame[0:4], 24)
	if _, err := parseNetlinkRouteMessages(shortFrame); err == nil {
		t.Fatal("parseNetlinkRouteMessages accepted a short frame")
	}
}

func TestParseNetlinkRouteRejectsBadAttribute(t *testing.T) {
	body := make([]byte, routeMessageLength)
	body[0] = 2
	body = append(body, 3, 0, 1, 0) // length 3, type 1
	if _, err := parseNetlinkRoute(body); err == nil {
		t.Fatal("parseNetlinkRoute accepted a truncated attribute")
	}
}

func TestMarshalNetlinkRouteRequest(t *testing.T) {
	request := marshalNetlinkRouteRequest(9, 2, testIPv4(), 0, false)
	if got := binary.NativeEndian.Uint16(request[4:6]); got != netlinkMessageGetRoute {
		t.Errorf("message type = %d, want RTM_GETROUTE", got)
	}
	if got := binary.NativeEndian.Uint32(request[8:12]); got != 9 {
		t.Errorf("sequence = %d, want 9", got)
	}
	body := request[netlinkHeaderLength:]
	if body[0] != 2 || body[1] != 32 {
		t.Errorf("route request family/dst length = %d/%d", body[0], body[1])
	}
	if len(body) != routeMessageLength+8 || body[routeMessageLength+2] != routeAttrDst {
		t.Errorf("route request attributes = % x", body[routeMessageLength:])
	}

	dump := marshalNetlinkRouteRequest(10, 2, testIPv4Zero(), MainTable, true)
	if flags := binary.NativeEndian.Uint16(dump[6:8]); flags&0x300 != 0x300 {
		t.Errorf("dump flags = %#x, want NLM_F_DUMP", flags)
	}
	if body := dump[netlinkHeaderLength:]; body[4] != byte(MainTable) {
		t.Errorf("dump table = %d, want %d", body[4], MainTable)
	}
}

func TestDecideQueriedEgressUsesPhysicalMainDefault(t *testing.T) {
	routes := []Route{
		// Result of RTM_GETROUTE for 1.1.1.1 through policy table 2022.
		{Family: 4, Table: 2022, Device: "cxtun", Metric: 0, Tun: true},
		{Family: 4, Table: MainTable, Device: "eth0", Metric: 100},
	}
	rules := []Rule{{Family: 4, Priority: 0, Table: 2022, MatchAll: true}}
	detection, ok := Decide(routes, rules)
	if !ok || detection.Tun != "cxtun" || detection.Physical != "eth0" {
		t.Fatalf("detection = %#v, %v; want cxtun -> eth0", detection, ok)
	}
}

func TestIsTunNameRecognizesCxTun(t *testing.T) {
	for _, name := range []string{"cxtun", "Meta", "utun3", "wg0"} {
		if !IsTunName(name) {
			t.Errorf("IsTunName(%q) = false", name)
		}
	}
	if IsTunName("eth0") {
		t.Error("IsTunName(eth0) = true")
	}
}

func testNetlinkRouteFrame(family uint8, dstLength int, table int, ifIndex, metric uint32, attrTable uint32) []byte {
	body := make([]byte, routeMessageLength)
	body[0] = family
	body[1] = byte(dstLength)
	body[4] = byte(table)
	body = appendTestAttribute(body, routeAttrOIF, uint32Bytes(ifIndex))
	body = appendTestAttribute(body, routeAttrPriority, uint32Bytes(metric))
	if attrTable != 0 {
		body = appendTestAttribute(body, routeAttrTable, uint32Bytes(attrTable))
	}

	frame := make([]byte, netlinkHeaderLength)
	binary.NativeEndian.PutUint32(frame[0:4], uint32(netlinkHeaderLength+len(body)))
	binary.NativeEndian.PutUint16(frame[4:6], netlinkMessageNewRoute)
	binary.NativeEndian.PutUint32(frame[8:12], 1)
	return append(frame, body...)
}

func appendTestAttribute(buffer []byte, attributeType uint16, value []byte) []byte {
	length := 4 + len(value)
	start := len(buffer)
	buffer = append(buffer, make([]byte, alignNetlink(length))...)
	binary.NativeEndian.PutUint16(buffer[start:start+2], uint16(length))
	binary.NativeEndian.PutUint16(buffer[start+2:start+4], attributeType)
	copy(buffer[start+4:], value)
	return buffer
}

func uint32Bytes(value uint32) []byte {
	var data [4]byte
	binary.NativeEndian.PutUint32(data[:], value)
	return data[:]
}

func testIPv4() netip.Addr {
	return netip.MustParseAddr("1.1.1.1")
}

func testIPv4Zero() netip.Addr {
	return netip.Addr{}
}
