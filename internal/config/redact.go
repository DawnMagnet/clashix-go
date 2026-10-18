package config

import "strings"

// Redacted replaces sensitive values in output produced by "cx config show".
const Redacted = "REDACTED"

var sensitiveKeys = map[string]struct{}{
	"auth":           {},
	"authentication": {},
	"password":       {},
	"private-key":    {},
	"private_key":    {},
	"psk":            {},
	"secret":         {},
	"token":          {},
	"username":       {},
	"uuid":           {},
}

// Redact returns a deep copy of cfg with controller secrets and proxy
// credentials replaced by Redacted.
func Redact(cfg map[string]any) map[string]any {
	if cfg == nil {
		return nil
	}
	out := make(map[string]any, len(cfg))
	for key, value := range cfg {
		if _, sensitive := sensitiveKeys[strings.ToLower(key)]; sensitive {
			out[key] = Redacted
			continue
		}
		out[key] = redactValue(value)
	}
	return out
}

func redactValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return Redact(value)
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = redactValue(item)
		}
		return out
	default:
		return value
	}
}
