package config

import (
	"hash/fnv"
	"net/netip"
	"strings"
)

// DefaultTunDevice is mihomo's default TUN interface name.
const DefaultTunDevice = "Meta"

// DefaultFakeIPRange is mihomo's default dns.fake-ip-range. Its first /30 is
// also the default TUN IPv4 address.
var DefaultFakeIPRange = netip.MustParsePrefix("198.18.0.1/16")

// DefaultTunInet6 is mihomo's default TUN IPv6 prefix.
var DefaultTunInet6 = netip.MustParsePrefix("fdfe:dcba:9876::1/126")

// IsNonDefaultTunDevice reports whether a device name needs its own addresses
// and iproute2 indexes. Empty, mihomo's per-OS defaults ("Meta", "utunN"), and
// cx's packaged example device "cxtun" keep 198.18.0.1/16 and 2022/9000.
func IsNonDefaultTunDevice(device string) bool {
	device = strings.ToLower(strings.TrimSpace(device))
	switch {
	case device == "":
		return false
	case device == "meta", device == "cxtun":
		return false
	case strings.HasPrefix(device, "utun"):
		return false
	default:
		return true
	}
}

func tunAddressHash(value string) uint32 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(value))
	return hash.Sum32()
}

// DeriveTunFakeIPRange picks a deterministic 198.19.N.1/24 slice outside
// mihomo's default 198.18.0.0/16 fake-IP pool for a non-default device.
func DeriveTunFakeIPRange(device string) netip.Prefix {
	if !IsNonDefaultTunDevice(device) {
		return DefaultFakeIPRange
	}
	n := byte(tunAddressHash("fakeip:"+device) % 256)
	return netip.PrefixFrom(netip.AddrFrom4([4]byte{198, 19, n, 1}), 24)
}

// DeriveTunInet6 picks a deterministic ULA /126 for a non-default device,
// avoiding mihomo's default fdfe:dcba:9876::1/126.
func DeriveTunInet6(device string) netip.Prefix {
	if !IsNonDefaultTunDevice(device) {
		return DefaultTunInet6
	}
	high := tunAddressHash("inet6:" + device)
	low := tunAddressHash("inet6-low:" + device)
	bytes := [16]byte{
		0xfd,
		byte(high >> 24), byte(high >> 16), byte(high >> 8), byte(high),
		byte(low >> 24), byte(low >> 16), byte(low >> 8),
		0, 0, 0, 0, 0, 0, 0, 1,
	}
	for {
		prefix := netip.PrefixFrom(netip.AddrFrom16(bytes), 126)
		if prefix.Masked() != DefaultTunInet6.Masked() {
			return prefix
		}
		bytes[14]++
	}
}
