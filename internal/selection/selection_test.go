package selection

import (
	"strings"
	"testing"

	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/adapter/outbound"
	"github.com/metacubex/mihomo/adapter/outboundgroup"
	C "github.com/metacubex/mihomo/constant"
	P "github.com/metacubex/mihomo/constant/provider"
	"github.com/metacubex/mihomo/tunnel"
)

func TestMatcher(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		candidate string
		want      bool
	}{
		{name: "all terms substrings", pattern: "hy2 us", candidate: "🇺🇸 [Hy2]US 01", want: true},
		{name: "case insensitive", pattern: "HY2 us", candidate: "🇺🇸 [hy2]US 01", want: true},
		{name: "subsequence fallback", pattern: "h2u", candidate: "Hy2 US 01", want: true},
		{name: "missing term", pattern: "hy2 jp", candidate: "🇺🇸 [Hy2]US 01", want: false},
		{name: "regex", pattern: "re:^🇯🇵 .* 0[1-3]$", candidate: "🇯🇵 Tokyo 02", want: true},
		{name: "regex is case insensitive", pattern: "re:tokyo", candidate: "🇯🇵 TOKYO 02", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			matcher, err := newMatcher(test.pattern)
			if err != nil {
				t.Fatalf("newMatcher: %v", err)
			}
			if got := matcher.matches(test.candidate); got != test.want {
				t.Errorf("matches(%q, %q) = %v, want %v", test.pattern, test.candidate, got, test.want)
			}
		})
	}
}

func TestMatcherPrefersTermMatchOverEarlierFuzzyCandidate(t *testing.T) {
	matcher, err := newMatcher("ab")
	if err != nil {
		t.Fatalf("newMatcher: %v", err)
	}
	got, ok := matcher.match([]string{"aXb", "AB target"})
	if !ok || got != "AB target" {
		t.Fatalf("match = %q, %v; want the substring candidate", got, ok)
	}
}

func TestParseSpecs(t *testing.T) {
	specs, err := ParseSpecs([]string{"node", "Proxy=node", "Proxy=re:^node-[0-9]+$", "re:^prefix=value$"})
	if err != nil {
		t.Fatalf("ParseSpecs: %v", err)
	}
	if specs[0].Group != "" || specs[0].Pattern != "node" {
		t.Errorf("plain spec = %#v", specs[0])
	}
	if specs[1].Group != "Proxy" || specs[1].Pattern != "node" {
		t.Errorf("group spec = %#v", specs[1])
	}
	if specs[2].Pattern != "re:^node-[0-9]+$" {
		t.Errorf("group regex spec = %#v", specs[2])
	}
	if specs[3].Group != "" || specs[3].Pattern != "re:^prefix=value$" {
		t.Errorf("regex with equals = %#v", specs[3])
	}

	for _, invalid := range []string{"", "Group=", "=node", "re:["} {
		if _, err := ParseSpecs([]string{invalid}); err == nil {
			t.Errorf("ParseSpecs(%q) succeeded, want error", invalid)
		}
	}
}

func TestPrimaryGroupName(t *testing.T) {
	groups := []groupInfo{
		{name: "Auto", kind: "url-test"},
		{name: "First", kind: "select"},
		{name: "Final", kind: "select"},
	}
	cfg := map[string]any{
		"rules": []any{"DOMAIN,example.test,First", "MATCH,Final"},
	}
	if got := primaryGroupName(cfg, groups); got != "Final" {
		t.Errorf("primaryGroupName = %q, want Final", got)
	}

	cfg["rules"] = []any{"MATCH,Auto"}
	if got := primaryGroupName(cfg, groups); got != "First" {
		t.Errorf("primaryGroupName with non-select MATCH = %q, want First", got)
	}
}

func TestPlaceholderInfo(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    bool
	}{
		{name: "node", address: "198.51.100.10:443"},
		{name: "remaining traffic", address: "198.51.100.10:443", want: true},
		{name: "expire at", address: "198.51.100.10:443", want: true},
		{name: "reserved server", address: "10.255.255.255:443", want: true},
		{name: "unspecified server", address: "0.0.0.0:443", want: true},
		{name: "empty server", address: "", want: true},
		{name: "normal server", address: "203.0.113.5:8443"},
	}
	for _, test := range tests {
		if got := isPlaceholderInfo(test.name, test.address); got != test.want {
			t.Errorf("isPlaceholderInfo(%q, %q) = %v, want %v", test.name, test.address, got, test.want)
		}
	}
}

func TestClosestCandidates(t *testing.T) {
	candidates := []string{"🇯🇵 Tokyo 01", "🇯🇵 Tokyo 02", "🇯🇵 Osaka 01", "🇸🇬 Singapore 01", "🇺🇸 New York 01", "🇩🇪 Frankfurt 01"}
	got := closest("tokio", candidates, 5)
	if len(got) != 5 {
		t.Fatalf("closest returned %d candidates, want 5: %v", len(got), got)
	}
	if got[0] != "🇯🇵 Tokyo 01" && got[0] != "🇯🇵 Tokyo 02" {
		t.Errorf("closest first candidate = %q, want a Tokyo node", got[0])
	}
}

func TestApplySelectionAndPlaceholders(t *testing.T) {
	proxyMap := testProxyMap(t,
		[]map[string]any{
			{"name": "remaining", "type": "ss", "server": "10.255.255.255", "port": 443, "cipher": "aes-128-gcm", "password": "made-up"},
			{"name": "🇺🇸 [Hy2]US 01", "type": "ss", "server": "198.51.100.10", "port": 443, "cipher": "aes-128-gcm", "password": "made-up"},
			{"name": "🇯🇵 Tokyo 01", "type": "ss", "server": "198.51.100.20", "port": 443, "cipher": "aes-128-gcm", "password": "made-up"},
		},
		[]map[string]any{
			{"name": "♻️ Info", "type": "select", "proxies": []any{"remaining", "🇯🇵 Tokyo 01"}},
			{"name": "🚀 Proxy", "type": "select", "proxies": []any{"remaining", "🇺🇸 [Hy2]US 01", "🇯🇵 Tokyo 01"}},
		},
	)
	cfg := map[string]any{
		"rules": []any{"MATCH,🚀 Proxy"},
		"proxy-groups": []any{
			map[string]any{"name": "♻️ Info", "type": "select", "proxies": []any{"remaining", "🇯🇵 Tokyo 01"}},
			map[string]any{"name": "🚀 Proxy", "type": "select", "proxies": []any{"remaining", "🇺🇸 [Hy2]US 01", "🇯🇵 Tokyo 01"}},
		},
	}
	t.Cleanup(func() { tunnel.UpdateProxies(nil, nil) })

	if err := Apply(cfg, []string{"hy2 us"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := groupNow(t, proxyMap, "🚀 Proxy"); got != "🇺🇸 [Hy2]US 01" {
		t.Errorf("primary selection = %q, want the Hy2 node", got)
	}
	if got := groupNow(t, proxyMap, "♻️ Info"); got != "🇯🇵 Tokyo 01" {
		t.Errorf("placeholder fallback = %q, want first non-placeholder", got)
	}
}

func TestApplyDefaultSkipsPlaceholders(t *testing.T) {
	proxyMap := testProxyMap(t,
		[]map[string]any{
			{"name": "套餐到期", "type": "ss", "server": "198.51.100.10", "port": 443, "cipher": "aes-128-gcm", "password": "made-up"},
			{"name": "🇩🇪 Frankfurt 01", "type": "ss", "server": "198.51.100.30", "port": 443, "cipher": "aes-128-gcm", "password": "made-up"},
			{"name": "remaining", "type": "ss", "server": "10.255.255.255", "port": 443, "cipher": "aes-128-gcm", "password": "made-up"},
			{"name": "🇸🇬 Singapore 01", "type": "ss", "server": "198.51.100.40", "port": 443, "cipher": "aes-128-gcm", "password": "made-up"},
		},
		[]map[string]any{
			{"name": "Proxy", "type": "select", "proxies": []any{"套餐到期", "🇩🇪 Frankfurt 01"}},
			{"name": "Other", "type": "select", "proxies": []any{"remaining", "🇸🇬 Singapore 01"}},
		},
	)
	cfg := map[string]any{"proxy-groups": []any{
		map[string]any{"name": "Proxy", "type": "select", "proxies": []any{"套餐到期", "🇩🇪 Frankfurt 01"}},
		map[string]any{"name": "Other", "type": "select", "proxies": []any{"remaining", "🇸🇬 Singapore 01"}},
	}}
	t.Cleanup(func() { tunnel.UpdateProxies(nil, nil) })

	if err := Apply(cfg, nil); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := groupNow(t, proxyMap, "Proxy"); got != "🇩🇪 Frankfurt 01" {
		t.Errorf("Proxy = %q, want placeholder skipped", got)
	}
	if got := groupNow(t, proxyMap, "Other"); got != "🇸🇬 Singapore 01" {
		t.Errorf("Other = %q, want placeholder skipped", got)
	}
}

func TestApplyNestedGroupAndNoMatch(t *testing.T) {
	proxyMap := testProxyMap(t,
		[]map[string]any{
			{"name": "node-a", "type": "ss", "server": "198.51.100.10", "port": 443, "cipher": "aes-128-gcm", "password": "made-up"},
			{"name": "node-b", "type": "ss", "server": "198.51.100.20", "port": 443, "cipher": "aes-128-gcm", "password": "made-up"},
		},
		[]map[string]any{
			{"name": "Auto", "type": "select", "proxies": []any{"node-a", "node-b"}},
			{"name": "Main", "type": "select", "proxies": []any{"Auto"}},
		},
	)
	cfg := map[string]any{
		"rules": []any{"MATCH,Main"},
		"proxy-groups": []any{
			map[string]any{"name": "Auto", "type": "select", "proxies": []any{"node-a", "node-b"}},
			map[string]any{"name": "Main", "type": "select", "proxies": []any{"Auto"}},
		},
	}
	t.Cleanup(func() { tunnel.UpdateProxies(nil, nil) })

	if err := Apply(cfg, []string{"node-b"}); err != nil {
		t.Fatalf("Apply nested: %v", err)
	}
	if got := groupNow(t, proxyMap, "Auto"); got != "node-b" {
		t.Errorf("nested selection = %q, want node-b", got)
	}

	err := Apply(cfg, []string{"no-such-node"})
	if err == nil {
		t.Fatal("Apply succeeded without a matching candidate")
	}
	for _, want := range []string{"closest candidates", "node-a", "node-b"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestApplyGroupMatcher(t *testing.T) {
	proxyMap := testProxyMap(t,
		[]map[string]any{
			{"name": "node-a", "type": "ss", "server": "198.51.100.10", "port": 443, "cipher": "aes-128-gcm", "password": "made-up"},
			{"name": "node-b", "type": "ss", "server": "198.51.100.20", "port": 443, "cipher": "aes-128-gcm", "password": "made-up"},
		},
		[]map[string]any{
			{"name": "Automatic", "type": "select", "proxies": []any{"node-a", "node-b"}},
		},
	)
	cfg := map[string]any{"proxy-groups": []any{
		map[string]any{"name": "Automatic", "type": "select", "proxies": []any{"node-a", "node-b"}},
	}}
	t.Cleanup(func() { tunnel.UpdateProxies(nil, nil) })

	if err := Apply(cfg, []string{"auto=node-b"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := groupNow(t, proxyMap, "Automatic"); got != "node-b" {
		t.Errorf("group selection = %q, want node-b", got)
	}
	if err := Apply(cfg, []string{"missing=node-a"}); err == nil || !strings.Contains(err.Error(), "closest groups") {
		t.Errorf("missing group error = %v, want closest groups", err)
	}
}

func testProxyMap(t *testing.T, proxies []map[string]any, groups []map[string]any) map[string]C.Proxy {
	t.Helper()
	proxyMap := map[string]C.Proxy{
		"COMPATIBLE": adapter.NewProxy(outbound.NewCompatible()),
	}
	allProxies := []string{"COMPATIBLE"}
	for _, mapping := range proxies {
		proxy, err := adapter.ParseProxy(mapping)
		if err != nil {
			t.Fatalf("parse test proxy %v: %v", mapping["name"], err)
		}
		proxyMap[proxy.Name()] = proxy
		allProxies = append(allProxies, proxy.Name())
	}
	providers := map[string]P.ProxyProvider{}
	for _, mapping := range groups {
		group, err := outboundgroup.ParseProxyGroup(mapping, proxyMap, providers, allProxies, nil)
		if err != nil {
			t.Fatalf("parse test group %v: %v", mapping["name"], err)
		}
		proxyMap[group.Name()] = adapter.NewProxy(group)
		allProxies = append(allProxies, group.Name())
	}
	tunnel.UpdateProxies(proxyMap, providers)
	return proxyMap
}

func groupNow(t *testing.T, proxyMap map[string]C.Proxy, name string) string {
	t.Helper()
	proxy, ok := proxyMap[name]
	if !ok {
		t.Fatalf("group %q is missing", name)
	}
	group, ok := proxy.Adapter().(outboundgroup.ProxyGroup)
	if !ok {
		t.Fatalf("%q is not a proxy group", name)
	}
	return group.Now()
}
