package config

import "strings"

// HasProxyCredentials reports whether a resolved configuration contains
// material that should not be published. subscriptionUsed covers the case
// where subscriptions were resolved into the configuration at pack time.
func HasProxyCredentials(cfg map[string]any, subscriptionUsed bool) bool {
	if cfg == nil {
		return subscriptionUsed
	}
	if subscriptionUsed {
		return true
	}
	if proxies, ok := proxiesOf(cfg); ok && len(proxies) > 0 {
		return true
	}

	providers, ok := cfg["proxy-providers"].(map[string]any)
	if !ok {
		return false
	}
	for _, value := range providers {
		provider, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if url, ok := provider["url"].(string); ok && strings.TrimSpace(url) != "" {
			return true
		}
	}
	return false
}
