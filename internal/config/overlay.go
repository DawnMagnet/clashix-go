package config

// Overlay contains the configuration keys controlled by cx. Nil pointer fields
// are left untouched.
type Overlay struct {
	Port               *int
	SocksPort          *int
	MixedPort          *int
	RedirPort          *int
	TProxyPort         *int
	AllowLAN           *bool
	Mode               *string
	LogLevel           *string
	ExternalController *string
	Secret             *string
	InterfaceName      *string
	RoutingMark        *int

	TunEnable        *bool
	TunStack         *string
	TunExcludeRoutes []string
}

// ApplyOverlay writes the controlled keys into cfg. Unknown keys and all
// other values are preserved.
func ApplyOverlay(cfg map[string]any, overlay Overlay) {
	if cfg == nil {
		return
	}
	if overlay.Port != nil {
		cfg["port"] = *overlay.Port
	}
	if overlay.SocksPort != nil {
		cfg["socks-port"] = *overlay.SocksPort
	}
	if overlay.MixedPort != nil {
		cfg["mixed-port"] = *overlay.MixedPort
	}
	if overlay.RedirPort != nil {
		cfg["redir-port"] = *overlay.RedirPort
	}
	if overlay.TProxyPort != nil {
		cfg["tproxy-port"] = *overlay.TProxyPort
	}
	if overlay.AllowLAN != nil {
		cfg["allow-lan"] = *overlay.AllowLAN
	}
	if overlay.Mode != nil {
		cfg["mode"] = *overlay.Mode
	}
	if overlay.LogLevel != nil {
		cfg["log-level"] = *overlay.LogLevel
	}
	if overlay.ExternalController != nil {
		cfg["external-controller"] = *overlay.ExternalController
	}
	if overlay.Secret != nil {
		cfg["secret"] = *overlay.Secret
	}
	if overlay.InterfaceName != nil {
		cfg["interface-name"] = *overlay.InterfaceName
	}
	if overlay.RoutingMark != nil {
		cfg["routing-mark"] = *overlay.RoutingMark
	}
	if overlay.TunEnable != nil || overlay.TunStack != nil || overlay.TunExcludeRoutes != nil {
		tun := ensureMap(cfg, "tun")
		if overlay.TunEnable != nil {
			tun["enable"] = *overlay.TunEnable
		}
		if overlay.TunStack != nil {
			tun["stack"] = *overlay.TunStack
		}
		if overlay.TunExcludeRoutes != nil {
			routes := make([]any, len(overlay.TunExcludeRoutes))
			for i, route := range overlay.TunExcludeRoutes {
				routes[i] = route
			}
			tun["route-exclude-address"] = routes
		}
		if overlay.TunStack != nil || overlay.TunExcludeRoutes != nil || (overlay.TunEnable != nil && *overlay.TunEnable) {
			tun["auto-route"] = true
			tun["auto-detect-interface"] = true
		}
	}
}

func ensureMap(cfg map[string]any, key string) map[string]any {
	if value, ok := cfg[key].(map[string]any); ok {
		return value
	}
	value := map[string]any{}
	cfg[key] = value
	return value
}

// Apply is a convenience method for ApplyOverlay.
func (o Overlay) Apply(cfg map[string]any) {
	ApplyOverlay(cfg, o)
}

// IntPtr returns a pointer to value, for use in an Overlay.
func IntPtr(value int) *int { return &value }

// BoolPtr returns a pointer to value, for use in an Overlay.
func BoolPtr(value bool) *bool { return &value }

// StringPtr returns a pointer to value, for use in an Overlay.
func StringPtr(value string) *string { return &value }
