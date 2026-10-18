package config

import (
	"hash/fnv"
	"net/netip"
	"strings"
)

// DefaultTunDevice is mihomo's default TUN interface name.
const DefaultTunDevice = "Meta"

// DefaultTunInet4 is the /30 mihomo derives from the default
// dns.fake-ip-range (198.18.0.1/16).
var DefaultTunInet4 = netip.MustParsePrefix("198.18.0.1/30")

// DefaultTunInet6 is mihomo's default TUN IPv6 prefix.
var DefaultTunInet6 = netip.MustParsePrefix("fdfe:dcba:9876::1/126")

// IsDefaultTunDevice reports whether device is empty or mihomo's default name.
func IsDefaultTunDevice(device string) bool {
	device = strings.TrimSpace(device)
	return device == "" || strings.EqualFold(device, DefaultTunDevice)
}

func tunAddressHash(value string) uint32 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(value))
	return hash.Sum32()
}

// DeriveTunInet4 picks a deterministic /30 from 198.18.0.0/16 other than the
// default 198.18.0.1/30 so two non-default TUN devices do not share an address.
func DeriveTunInet4(device string) netip.Prefix {
	const subnets = 1 << 14 // number of /30 blocks in a /16
	index := uint32(tunAddressHash("inet4:"+device) % subnets)
	if index == 0 {
		index = 1
	}
	last := byte(index&0x3f)<<2 | 1
	return netip.PrefixFrom(netip.AddrFrom4([4]byte{198, 18, byte(index >> 6), last}), 30)
}

// DeriveTunInet6 picks a deterministic ULA /126 for a non-default device,
// avoiding mihomo's default fdfe:dcba:9876::1/126.
func DeriveTunInet6(device string) netip.Prefix {
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
