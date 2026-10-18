package hostnet

import (
	"encoding/binary"
	"fmt"
	"net/netip"
)

// Netlink wire constants used by the host-route reader. They are spelled out
// here so the message parser can be tested on every platform.
const (
	netlinkHeaderLength    = 16
	routeMessageLength     = 12
	netlinkMessageError    = 2
	netlinkMessageDone     = 3
	netlinkMessageNewRoute = 24
	netlinkMessageGetRoute = 26

	routeAttrDst      = 1
	routeAttrOIF      = 4
	routeAttrPriority = 6
	routeAttrTable    = 15
)

type netlinkRoute struct {
	Family    int
	DstLength int
	Table     int
	IfIndex   uint32
	Metric    uint32
}

func alignNetlink(length int) int {
	return (length + 3) &^ 3
}

// parseNetlinkRouteMessages parses one or more RTM_NEWROUTE messages from a
// complete netlink datagram. Control messages are ignored.
func parseNetlinkRouteMessages(data []byte) ([]netlinkRoute, error) {
	var routes []netlinkRoute
	for len(data) >= netlinkHeaderLength {
		length := int(binary.NativeEndian.Uint32(data[0:4]))
		if length < netlinkHeaderLength || length > len(data) {
			return nil, fmt.Errorf("invalid netlink message length %d", length)
		}
		messageType := binary.NativeEndian.Uint16(data[4:6])
		if messageType == netlinkMessageNewRoute {
			route, err := parseNetlinkRoute(data[netlinkHeaderLength:length])
			if err != nil {
				return nil, err
			}
			routes = append(routes, route)
		}
		next := alignNetlink(length)
		if next <= 0 || next > len(data) {
			break
		}
		data = data[next:]
	}
	return routes, nil
}

// parseNetlinkRoute parses the rtmsg body and its attributes.
func parseNetlinkRoute(data []byte) (netlinkRoute, error) {
	if len(data) < routeMessageLength {
		return netlinkRoute{}, fmt.Errorf("netlink route message is too short")
	}
	route := netlinkRoute{
		DstLength: int(data[1]),
		Table:     int(data[4]),
	}
	switch data[0] {
	case 2: // AF_INET
		route.Family = 4
	case 10: // AF_INET6
		route.Family = 6
	default:
		return netlinkRoute{}, fmt.Errorf("unsupported netlink address family %d", data[0])
	}
	if route.Table == 0 {
		route.Table = MainTable
	}

	attributes := data[routeMessageLength:]
	for len(attributes) >= 4 {
		length := int(binary.NativeEndian.Uint16(attributes[0:2]))
		attributeType := binary.NativeEndian.Uint16(attributes[2:4])
		if attributeType == 0 && length == 0 {
			break
		}
		if length < 4 || length > len(attributes) {
			return netlinkRoute{}, fmt.Errorf("invalid netlink route attribute length %d", length)
		}
		value := attributes[4:length]
		switch attributeType {
		case routeAttrOIF:
			if len(value) >= 4 {
				route.IfIndex = binary.NativeEndian.Uint32(value[:4])
			}
		case routeAttrPriority:
			if len(value) >= 4 {
				route.Metric = binary.NativeEndian.Uint32(value[:4])
			}
		case routeAttrTable:
			if len(value) >= 4 {
				route.Table = int(binary.NativeEndian.Uint32(value[:4]))
			}
		}
		next := alignNetlink(length)
		if next > len(attributes) {
			break
		}
		attributes = attributes[next:]
	}
	return route, nil
}

// marshalNetlinkRouteRequest builds an RTM_GETROUTE request. A valid
// destination performs a route lookup; table with dump requests all routes in
// that table.
func marshalNetlinkRouteRequest(sequence uint32, family uint8, destination netip.Addr, table int, dump bool) []byte {
	body := make([]byte, routeMessageLength)
	body[0] = family
	body[4] = byte(table)
	if destination.IsValid() {
		body[1] = byte(destination.BitLen())
		body = appendNetlinkAttribute(body, routeAttrDst, destination.AsSlice())
	}
	if table > 0 {
		var value [4]byte
		binary.NativeEndian.PutUint32(value[:], uint32(table))
		body = appendNetlinkAttribute(body, routeAttrTable, value[:])
	}

	flags := uint16(1) // NLM_F_REQUEST
	if dump {
		flags |= 0x300 // NLM_F_ROOT | NLM_F_MATCH
	}
	message := make([]byte, netlinkHeaderLength, netlinkHeaderLength+len(body))
	binary.NativeEndian.PutUint32(message[0:4], uint32(netlinkHeaderLength+len(body)))
	binary.NativeEndian.PutUint16(message[4:6], netlinkMessageGetRoute)
	binary.NativeEndian.PutUint16(message[6:8], flags)
	binary.NativeEndian.PutUint32(message[8:12], sequence)
	return append(message, body...)
}

func appendNetlinkAttribute(buffer []byte, attributeType uint16, value []byte) []byte {
	length := 4 + len(value)
	start := len(buffer)
	buffer = append(buffer, make([]byte, alignNetlink(length))...)
	binary.NativeEndian.PutUint16(buffer[start:start+2], uint16(length))
	binary.NativeEndian.PutUint16(buffer[start+2:start+4], attributeType)
	copy(buffer[start+4:], value)
	return buffer
}
