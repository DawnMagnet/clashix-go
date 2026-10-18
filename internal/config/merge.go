package config

// MergeSubscriptions implements the Clashix subscription merge rules: the
// first document is the base configuration and later documents contribute
// only their proxies.
func MergeSubscriptions(configs ...map[string]any) map[string]any {
	if len(configs) == 0 {
		return map[string]any{}
	}

	result := Clone(configs[0])
	if result == nil {
		result = map[string]any{}
	}

	proxies, _ := proxiesOf(result)
	if len(proxies) > 0 {
		proxies = append([]any(nil), proxies...)
	}
	for _, cfg := range configs[1:] {
		more, ok := proxiesOf(cfg)
		if !ok {
			continue
		}
		proxies = append(proxies, more...)
	}
	if len(proxies) > 0 {
		result["proxies"] = proxies
	}
	return result
}

func proxiesOf(cfg map[string]any) ([]any, bool) {
	if cfg == nil {
		return nil, false
	}
	value, ok := cfg["proxies"]
	if !ok || value == nil {
		return nil, false
	}
	switch proxies := value.(type) {
	case []any:
		return proxies, true
	case []map[string]any:
		out := make([]any, len(proxies))
		for i, proxy := range proxies {
			out[i] = proxy
		}
		return out, true
	default:
		return nil, false
	}
}

// MergeBase merges a resolved subscription document over a local base
// configuration. Subscription keys take precedence, while base keys the
// subscription does not mention are preserved.
func MergeBase(base, subscription map[string]any) map[string]any {
	result := Clone(base)
	if result == nil {
		result = map[string]any{}
	}
	for key, value := range subscription {
		result[key] = cloneValue(value)
	}
	return result
}
