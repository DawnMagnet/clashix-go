package runner

import (
	"fmt"
	"strings"
)

// NoProxy is the proxy bypass list exported to child processes.
const NoProxy = "localhost,127.0.0.1,::1"

// ProxyEnv returns the lower- and upper-case proxy variables for a mixed
// loopback proxy port.
func ProxyEnv(port int) []string {
	httpProxy := fmt.Sprintf("http://127.0.0.1:%d", port)
	socksProxy := fmt.Sprintf("socks5h://127.0.0.1:%d", port)
	return []string{
		"http_proxy=" + httpProxy,
		"HTTP_PROXY=" + httpProxy,
		"https_proxy=" + httpProxy,
		"HTTPS_PROXY=" + httpProxy,
		"all_proxy=" + socksProxy,
		"ALL_PROXY=" + socksProxy,
		"no_proxy=" + NoProxy,
		"NO_PROXY=" + NoProxy,
	}
}

// BuildEnv returns base with proxy variables for port replaced and appended.
func BuildEnv(base []string, port int) []string {
	return MergeEnv(base, ProxyEnv(port))
}

// MergeEnv removes entries from base whose keys are overridden and appends the
// override entries. Proxy variable names are treated case-insensitively so
// stale upper- and lower-case entries are both replaced.
func MergeEnv(base, overrides []string) []string {
	keys := make(map[string]struct{}, len(overrides))
	for _, entry := range overrides {
		key, _, found := strings.Cut(entry, "=")
		if found {
			keys[strings.ToLower(key)] = struct{}{}
		}
	}

	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, found := strings.Cut(entry, "=")
		if found {
			if _, replaced := keys[strings.ToLower(key)]; replaced {
				continue
			}
		}
		result = append(result, entry)
	}
	return append(result, overrides...)
}
