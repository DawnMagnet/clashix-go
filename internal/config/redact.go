package config

import (
	"net/url"
	"strings"
)

// Redacted replaces sensitive values in output produced by "cx config show".
const Redacted = "REDACTED"

var sensitiveKeys = map[string]struct{}{
	"access-key":          {},
	"api-key":             {},
	"apikey":              {},
	"auth":                {},
	"auth-str":            {},
	"authentication":      {},
	"authorization":       {},
	"password":            {},
	"pre-shared-key":      {},
	"pre_shared_key":      {},
	"private-key":         {},
	"private_key":         {},
	"proxy-authorization": {},
	"psk":                 {},
	"secret":              {},
	"secret-key":          {},
	"token":               {},
	"username":            {},
	"uuid":                {},
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
	case string:
		if redacted, ok := redactURLUserinfo(value); ok {
			return redacted
		}
		return value
	default:
		return value
	}
}

func redactURLUserinfo(value string) (string, bool) {
	if !strings.Contains(value, "://") {
		return value, false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return value, false
	}

	changed := false
	if parsed.User != nil {
		parsed.User = url.User(Redacted)
		changed = true
	}
	if parsed.RawQuery != "" {
		parsed.RawQuery = Redacted
		changed = true
	}
	if parsed.Path != "" && parsed.Path != "/" {
		parsed.Path = "/" + Redacted
		parsed.RawPath = ""
		changed = true
	}
	if parsed.Fragment != "" {
		parsed.Fragment = Redacted
		changed = true
	}
	return parsed.String(), changed
}
