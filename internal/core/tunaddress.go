package core

import (
	"net/netip"
	"strings"

	"github.com/DawnMagnet/clashix-go/internal/config"
	mihomoconfig "github.com/metacubex/mihomo/config"
)

// applyParsedTunAddresses replaces mihomo's default TUN addresses for a
// non-default device. mihomo v1.19.32 ignores YAML tun.inet4-address and
// always derives it from dns.fake-ip-range, so the parsed config is the first
// place where the address can be changed. sing-tun assigns Inet4Address to the
// interface (its connected /30 route follows that address), and also uses it
// for Darwin auto-route subranges.
func applyParsedTunAddresses(cfg *mihomoconfig.Config, explicitInet6 netip.Prefix) {
	if cfg == nil || cfg.General == nil {
		return
	}
	tun := &cfg.General.Tun
	if !tun.Enable || config.IsDefaultTunDevice(tun.Device) {
		return
	}
	if shouldReplaceTunInet4(tun.Inet4Address) {
		tun.Inet4Address = []netip.Prefix{config.DeriveTunInet4(tun.Device)}
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

// shouldReplaceTunInet4 keeps a non-default dns.fake-ip-range /30. mihomo's
// default is 198.18.0.1/30.
func shouldReplaceTunInet4(addresses []netip.Prefix) bool {
	return len(addresses) == 0 || (len(addresses) == 1 && addresses[0].Masked() == config.DefaultTunInet4.Masked())
}

// shouldReplaceTunInet6 keeps an explicit non-default IPv6 prefix.
func shouldReplaceTunInet6(addresses []netip.Prefix) bool {
	return len(addresses) == 0 || (len(addresses) == 1 && addresses[0].Masked() == config.DefaultTunInet6.Masked())
}
