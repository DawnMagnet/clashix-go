package core

import (
	"net/netip"
	"strings"

	"github.com/DawnMagnet/clashix-go/internal/config"
	mihomoconfig "github.com/metacubex/mihomo/config"
)

// applyParsedTunAddresses replaces mihomo's default TUN IPv6 address for a
// non-default device. IPv4 is handled before parsing by moving
// dns.fake-ip-range to a separate 198.19.N.0/24 pool, which mihomo turns into
// the TUN /30 and the fake-IP allocator's reserved gateway.
func applyParsedTunAddresses(cfg *mihomoconfig.Config, explicitInet6 netip.Prefix) {
	if cfg == nil || cfg.General == nil {
		return
	}
	tun := &cfg.General.Tun
	if !tun.Enable || !config.IsNonDefaultTunDevice(tun.Device) {
		return
	}
	if cfg.General.IPv6 && shouldReplaceTunInet6(tun.Inet6Address) {
		if explicitInet6.IsValid() && explicitInet6.Masked() != config.DefaultTunInet6.Masked() {
			tun.Inet6Address = []netip.Prefix{explicitInet6}
		} else {
			tun.Inet6Address = []netip.Prefix{config.DeriveTunInet6(tun.Device)}
		}
	}
}

// explicitTunInet6 reads the YAML value before mihomo clears it when the host
// has no global IPv6 address, so an explicit non-default prefix still wins.
func explicitTunInet6(prepared []byte) netip.Prefix {
	cfg, err := config.Parse(prepared)
	if err != nil {
		return netip.Prefix{}
	}
	tun, ok := cfg["tun"].(map[string]any)
	if !ok {
		return netip.Prefix{}
	}
	switch value := tun["inet6-address"].(type) {
	case string:
		if prefix, err := netip.ParsePrefix(strings.TrimSpace(value)); err == nil {
			return prefix
		}
	case []any:
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				continue
			}
			if prefix, err := netip.ParsePrefix(strings.TrimSpace(text)); err == nil {
				return prefix
			}
		}
	}
	return netip.Prefix{}
}

// shouldReplaceTunInet6 keeps an explicit non-default IPv6 prefix.
func shouldReplaceTunInet6(addresses []netip.Prefix) bool {
	return len(addresses) == 0 || (len(addresses) == 1 && addresses[0].Masked() == config.DefaultTunInet6.Masked())
}
