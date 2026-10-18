package config

import (
	"net/url"
	"strings"
	"unicode"
)

// Redacted replaces sensitive values in output produced by "cx config show".
const Redacted = "REDACTED"

var sensitiveKeys = map[string]struct{}{
	"access-key":          {},
	"api-key":             {},
	"apikey":              {},
	"auth":                {},
	"auth-str":            {},
	"authorization":       {},
	"obfs-password":       {},
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
	"uuid":                {},
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(key)
	if _, sensitive := sensitiveKeys[key]; sensitive {
		return true
	}
	if _, sensitive := sensitiveKeys[strings.ReplaceAll(key, "_", "-")]; sensitive {
		return true
	}
	return strings.Contains(key, "token")
}

// Redact returns a deep copy of cfg with proxy credentials and URL secrets
// replaced by Redacted. Paths and ordinary query parameters are preserved.
func Redact(cfg map[string]any) map[string]any {
	if cfg == nil {
		return nil
	}
	return redactMap(cfg, nil)
}

func redactMap(cfg map[string]any, path []string) map[string]any {
	out := make(map[string]any, len(cfg))
	for key, value := range cfg {
		if isSensitiveKey(key) {
			out[key] = Redacted
			continue
		}
		childPath := appendPath(path, strings.ToLower(key))
		if text, ok := value.(string); ok {
			out[key] = redactURL(text, isSubscriptionPath(childPath))
			continue
		}
		out[key] = redactValue(value, childPath)
	}
	return out
}

func appendPath(path []string, element string) []string {
	out := make([]string, len(path)+1)
	copy(out, path)
	out[len(path)] = element
	return out
}

func redactValue(value any, path []string) any {
	switch value := value.(type) {
	case map[string]any:
		return redactMap(value, path)
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = redactValue(item, path)
		}
		return out
	case string:
		return redactURL(value, isSubscriptionPath(path))
	default:
		return value
	}
}

func isSubscriptionPath(path []string) bool {
	for _, element := range path {
		switch element {
		case "proxy-provider", "proxy-providers", "sub", "sub-url", "subscription", "subscription-url", "subscriptions":
			return true
		}
	}
	return false
}

func isSecretQueryKey(key string) bool {
	lower := strings.ToLower(key)
	for _, marker := range []string{"auth", "password", "secret", "sig", "token"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	if strings.HasSuffix(lower, "key") {
		return true
	}
	for _, part := range strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if part == "key" {
			return true
		}
	}
	return false
}

func redactURL(value string, subscription bool) string {
	if !strings.Contains(value, "://") {
		return value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return value
	}

	if parsed.User != nil {
		parsed.User = url.User(Redacted)
	}
	if parsed.RawQuery != "" {
		if subscription {
			parsed.RawQuery = Redacted
		} else if query, err := url.ParseQuery(parsed.RawQuery); err == nil {
			changed := false
			for key, values := range query {
				if !isSecretQueryKey(key) {
					continue
				}
				for i := range values {
					values[i] = Redacted
				}
				query[key] = values
				changed = true
			}
			if changed {
				parsed.RawQuery = query.Encode()
			}
		}
	}
	return parsed.String()
}
