package config

// Overlay contains the configuration keys controlled by cx. Nil pointer fields
// are left untouched.
type Overlay struct {
	Port               *int
	SocksPort          *int
	MixedPort          *int
	AllowLAN           *bool
	Mode               *string
	LogLevel           *string
	ExternalController *string
	Secret             *string
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
