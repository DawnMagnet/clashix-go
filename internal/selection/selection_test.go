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
		kind      string
		want      bool
	}{
		{name: "all terms substrings", pattern: "hy2 us", candidate: "🇺🇸 [Hy2]US 01", want: true},
		{name: "case insensitive", pattern: "HY2 us", candidate: "🇺🇸 [hy2]US 01", want: true},
		{name: "missing term", pattern: "hy2 jp", candidate: "🇺🇸 [Hy2]US 01", want: false},
		{name: "no subsequence fallback", pattern: "h2u", candidate: "Hy2 US 01", want: false},
		{name: "regex", pattern: "re:^🇯🇵 .* 0[1-3]$", candidate: "🇯🇵 Tokyo 02", want: true},
		{name: "regex is case insensitive", pattern: "re:tokyo", candidate: "🇯🇵 TOKYO 02", want: true},
		{name: "region alias japan", pattern: "japan", candidate: "🇯🇵 Tokyo 01", want: true},
		{name: "region alias chinese", pattern: "香港", candidate: "🇭🇰 Hong Kong 01", want: true},
		{name: "region alias hongkong", pattern: "hongkong", candidate: "🇭🇰 HK 01", want: true},
		{name: "region code token", pattern: "us", candidate: "🇺🇸 [Hy2]US 01", want: true},
		{name: "short code is not a substring", pattern: "us", candidate: "plus 01", want: false},
		{name: "region code maps to flag", pattern: "gb", candidate: "🇬🇧 London 01", want: true},
		{name: "type anytls", pattern: "type:anytls", candidate: "AnyTLS 01", kind: "AnyTLS", want: true},
		{name: "type hy2 alias", pattern: "type:hy2", candidate: "Hysteria 01", kind: "Hysteria2", want: true},
		{name: "type combined with region", pattern: "type:anytls hk", candidate: "🇭🇰 AnyTLS 01", kind: "AnyTLS", want: true},
		{name: "type mismatch", pattern: "type:anytls", candidate: "🇭🇰 AnyTLS 01", kind: "Hysteria2", want: false},
		{name: "bare protocol selects by type", pattern: "trojan", candidate: "TR 01", kind: "Trojan", want: true},
		{name: "bare protocol rejects other type", pattern: "trojan", candidate: "TR 01", kind: "VMess", want: false},
		{name: "bare shadowsocks alias", pattern: "shadowsocks", candidate: "SS 01", kind: "Shadowsocks", want: true},
		{name: "bare hy2 alias", pattern: "hy2", candidate: "Hysteria 01", kind: "Hysteria2", want: true},
		{name: "bare http protocol", pattern: "http", candidate: "HTTP 01", kind: "HTTP", want: true},
		{name: "three char term is not a substring", pattern: "syd", candidate: "Sydney 01", want: false},
		{name: "three char term matches a token", pattern: "syd", candidate: "SYD 01", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			matcher, err := newMatcher(test.pattern)
			if err != nil {
				t.Fatalf("newMatcher: %v", err)
			}
			if got := matcher.matchesCandidate(test.candidate, test.kind); got != test.want {
				t.Errorf("matches(%q, %q, %q) = %v, want %v", test.pattern, test.candidate, test.kind, got, test.want)
			}
		})
	}
}

func TestMatcherPrefersEarlierTermMatch(t *testing.T) {
	matcher, err := newMatcher("ab")
	if err != nil {
		t.Fatalf("newMatcher: %v", err)
	}
	got, ok := matcher.match([]string{"aXb", "AB target"})
	if !ok || got != "AB target" {
		t.Fatalf("match = %q, %v; want the whole-token candidate", got, ok)
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
	matcher, err := newMatcher("tokio")
	if err != nil {
		t.Fatalf("newMatcher: %v", err)
	}
	candidates := []nodeCandidate{
		{name: "🇯🇵 Tokyo 01"},
		{name: "🇯🇵 Tokyo 02"},
		{name: "🇯🇵 Osaka 01"},
		{name: "🇸🇬 Singapore 01"},
		{name: "🇺🇸 New York 01"},
		{name: "🇩🇪 Frankfurt 01"},
	}
	got := closest(matcher, candidates, 5)
	if len(got) != 5 {
		t.Fatalf("closest returned %d candidates, want 5: %v", len(got), got)
	}
	if got[0] != "🇯🇵 Tokyo 01" && got[0] != "🇯🇵 Tokyo 02" {
		t.Errorf("closest first candidate = %q, want a Tokyo node", got[0])
	}
}

func TestClosestRanksByMatchingWords(t *testing.T) {
	matcher, err := newMatcher("hy2 us")
	if err != nil {
		t.Fatalf("newMatcher: %v", err)
	}
	candidates := []nodeCandidate{
		{name: "🇯🇵 Tokyo 01"},
		{name: "🇺🇸 [Hy2]US 01"},
		{name: "🇺🇸 US 02"},
	}
	got := closest(matcher, candidates, 3)
	if got[0] != "🇺🇸 [Hy2]US 01" {
		t.Errorf("closest first candidate = %q, want the node matching both words", got[0])
	}
}

func TestClosestRanksByProtocolThenGroupOrder(t *testing.T) {
	matcher, err := newMatcher("type:anytls us")
	if err != nil {
		t.Fatalf("newMatcher: %v", err)
	}
	candidates := []nodeCandidate{
		{name: "🇺🇸 US 02", kind: "Shadowsocks"},
		{name: "🇺🇸 US 01", kind: "AnyTLS"},
		{name: "🇺🇸 US 03", kind: "AnyTLS"},
	}
	got := closest(matcher, candidates, 3)
	want := []string{"🇺🇸 US 01", "🇺🇸 US 03", "🇺🇸 US 02"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("closest = %v, want %v", got, want)
		}
	}
}

func TestClosestRanksProtocolAndRegionBeforeGroupOrder(t *testing.T) {
	matcher, err := newMatcher("trojan hk")
	if err != nil {
		t.Fatalf("newMatcher: %v", err)
	}
	candidates := []nodeCandidate{
		{name: "🇺🇸 US 01", kind: "Trojan"},
		{name: "🇯🇵 JP 01", kind: "Trojan"},
		{name: "🇭🇰 HK 01", kind: "Shadowsocks"},
		{name: "🇭🇰 HK 02", kind: "Trojan"},
	}
	got := closest(matcher, candidates, 4)
	want := []string{"🇭🇰 HK 02", "🇺🇸 US 01", "🇯🇵 JP 01", "🇭🇰 HK 01"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("closest = %v, want %v", got, want)
		}
	}
}

func TestClosestRanksBareTypeWord(t *testing.T) {
	matcher, err := newMatcher("trojan")
	if err != nil {
		t.Fatalf("newMatcher: %v", err)
	}
	candidates := []nodeCandidate{
		{name: "TR 01", kind: "VMess"},
		{name: "TR 02", kind: "VMess"},
		{name: "🇭🇰 HK 02", kind: "Trojan"},
		{name: "🇯🇵 JP 01", kind: "Trojan"},
	}
	got := closest(matcher, candidates, 4)
	want := []string{"🇭🇰 HK 02", "🇯🇵 JP 01", "TR 01", "TR 02"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("closest = %v, want %v", got, want)
		}
	}
}

func TestClosestRanksShortTermsOnTokenBoundaries(t *testing.T) {
	matcher, err := newMatcher("us")
	if err != nil {
		t.Fatalf("newMatcher: %v", err)
	}
	candidates := []nodeCandidate{
		{name: "plus 01", kind: "Shadowsocks"},
		{name: "🇺🇸 US 01", kind: "Shadowsocks"},
	}
	got := closest(matcher, candidates, 2)
	if got[0] != "🇺🇸 US 01" {
		t.Fatalf("closest = %v, want the US token first", got)
	}
}

func TestApplyBarePatternReportsUngroupedProxies(t *testing.T) {
	testProxyMap(t,
		[]map[string]any{
			{"name": "🇺🇸 US 01", "type": "ss", "server": "198.51.100.10", "port": 443, "cipher": "aes-128-gcm", "password": "made-up"},
			{"name": "🇺🇸 US 02", "type": "ss", "server": "198.51.100.20", "port": 443, "cipher": "aes-128-gcm", "password": "made-up"},
		},
		[]map[string]any{
			{"name": "Proxy", "type": "select", "proxies": []any{"🇺🇸 US 01"}},
		},
	)
	cfg := map[string]any{
		"proxies": []any{
			map[string]any{"name": "🇺🇸 US 01", "type": "ss"},
			map[string]any{"name": "🇺🇸 US 02", "type": "ss"},
		},
		"proxy-groups": []any{
			map[string]any{"name": "Proxy", "type": "select", "proxies": []any{"🇺🇸 US 01"}},
		},
	}
	t.Cleanup(func() { tunnel.UpdateProxies(nil, nil) })

	err := Apply(cfg, []string{"US 02"})
	if err == nil {
		t.Fatal("Apply succeeded without a select-group match")
	}
	for _, want := range []string{"matches 1 proxies that are not members of any select group", "🇺🇸 US 02", "closest candidates"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "🇺🇸 US 01, 🇺🇸 US 02") {
		t.Errorf("error ranked a grouped proxy as an ungrouped match: %q", err)
	}
}

func TestApplyRegionAndTypeSelection(t *testing.T) {
	proxyMap := testProxyMap(t,
		[]map[string]any{
			{"name": "🇯🇵 Tokyo 01", "type": "ss", "server": "198.51.100.10", "port": 443, "cipher": "aes-128-gcm", "password": "made-up"},
			{"name": "🇺🇸 AnyTLS US 01", "type": "anytls", "server": "198.51.100.20", "port": 443, "password": "made-up"},
			{"name": "Cyprus 01", "type": "ss", "server": "198.51.100.30", "port": 443, "cipher": "aes-128-gcm", "password": "made-up"},
		},
		[]map[string]any{
			{"name": "Proxy", "type": "select", "proxies": []any{"🇯🇵 Tokyo 01", "🇺🇸 AnyTLS US 01", "Cyprus 01"}},
		},
	)
	cfg := map[string]any{"proxy-groups": []any{
		map[string]any{"name": "Proxy", "type": "select", "proxies": []any{"🇯🇵 Tokyo 01", "🇺🇸 AnyTLS US 01", "Cyprus 01"}},
	}}
	t.Cleanup(func() { tunnel.UpdateProxies(nil, nil) })

	if err := Apply(cfg, []string{"us"}); err != nil {
		t.Fatalf("Apply region: %v", err)
	}
	if got := groupNow(t, proxyMap, "Proxy"); got != "🇺🇸 AnyTLS US 01" {
		t.Errorf("region selection = %q, want the US node", got)
	}
	if err := Apply(cfg, []string{"type:anytls"}); err != nil {
		t.Fatalf("Apply type: %v", err)
	}
	if got := groupNow(t, proxyMap, "Proxy"); got != "🇺🇸 AnyTLS US 01" {
		t.Errorf("type selection = %q, want the AnyTLS node", got)
	}
}

func TestApplyBareProtocolSelectsType(t *testing.T) {
	proxyMap := testProxyMap(t,
		[]map[string]any{
			{"name": "TR 01", "type": "ss", "server": "198.51.100.10", "port": 443, "cipher": "aes-128-gcm", "password": "made-up"},
			{"name": "🇭🇰 HK 01", "type": "trojan", "server": "198.51.100.20", "port": 443, "password": "made-up"},
		},
		[]map[string]any{
			{"name": "Proxy", "type": "select", "proxies": []any{"TR 01", "🇭🇰 HK 01"}},
		},
	)
	cfg := map[string]any{"proxy-groups": []any{
		map[string]any{"name": "Proxy", "type": "select", "proxies": []any{"TR 01", "🇭🇰 HK 01"}},
	}}
	t.Cleanup(func() { tunnel.UpdateProxies(nil, nil) })

	if err := Apply(cfg, []string{"trojan"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := groupNow(t, proxyMap, "Proxy"); got != "🇭🇰 HK 01" {
		t.Errorf("bare protocol selection = %q, want the Trojan node", got)
	}
}

func TestApplySelectionAndPlaceholders(t *testing.T) {
	proxyMap := testProxyMap(t,
		[]map[string]any{
			{"name": "remaining", "type": "ss", "server": "10.255.255.255", "port": 443, "cipher": "aes-128-gcm", "password": "made-up"},
			{"name": "🇺🇸 [Hy2]US 01", "type": "hysteria2", "server": "198.51.100.10", "port": 443, "password": "made-up"},
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

func TestApplyBarePatternAcrossSelectGroups(t *testing.T) {
	proxyMap := testProxyMap(t,
		[]map[string]any{
			{"name": "🇺🇸 [Hy2]US 01", "type": "hysteria2", "server": "198.51.100.10", "port": 443, "password": "made-up"},
			{"name": "🇯🇵 Tokyo 01", "type": "ss", "server": "198.51.100.20", "port": 443, "cipher": "aes-128-gcm", "password": "made-up"},
			{"name": "🇸🇬 Singapore 01", "type": "ss", "server": "198.51.100.30", "port": 443, "cipher": "aes-128-gcm", "password": "made-up"},
		},
		[]map[string]any{
			{"name": "Proxies", "type": "select", "proxies": []any{"🇺🇸 [Hy2]US 01", "🇯🇵 Tokyo 01"}},
			{"name": "Google", "type": "select", "proxies": []any{"Proxies", "DIRECT", "🇺🇸 [Hy2]US 01", "🇯🇵 Tokyo 01"}},
			{"name": "Media", "type": "select", "proxies": []any{"DIRECT", "🇺🇸 [Hy2]US 01", "🇯🇵 Tokyo 01"}},
			{"name": "AI", "type": "select", "proxies": []any{"Proxies", "DIRECT", "🇯🇵 Tokyo 01"}},
			{"name": "Other", "type": "select", "proxies": []any{"🇯🇵 Tokyo 01", "DIRECT"}},
		},
	)
	cfg := map[string]any{
		"rules": []any{"MATCH,Proxies"},
		"proxy-groups": []any{
			map[string]any{"name": "Proxies", "type": "select", "proxies": []any{"🇺🇸 [Hy2]US 01", "🇯🇵 Tokyo 01"}},
			map[string]any{"name": "Google", "type": "select", "proxies": []any{"Proxies", "DIRECT", "🇺🇸 [Hy2]US 01", "🇯🇵 Tokyo 01"}},
			map[string]any{"name": "Media", "type": "select", "proxies": []any{"DIRECT", "🇺🇸 [Hy2]US 01", "🇯🇵 Tokyo 01"}},
			map[string]any{"name": "AI", "type": "select", "proxies": []any{"Proxies", "DIRECT", "🇯🇵 Tokyo 01"}},
			map[string]any{"name": "Other", "type": "select", "proxies": []any{"🇯🇵 Tokyo 01", "DIRECT"}},
		},
	}
	t.Cleanup(func() { tunnel.UpdateProxies(nil, nil) })

	if err := Apply(cfg, []string{"hy2 us"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, group := range []string{"Proxies", "Google", "Media"} {
		if got := groupNow(t, proxyMap, group); got != "🇺🇸 [Hy2]US 01" {
			t.Errorf("%s = %q, want the matching Hy2 node", group, got)
		}
	}
	if got := groupNow(t, proxyMap, "AI"); got != "Proxies" {
		t.Errorf("AI = %q, want fallback to the primary group", got)
	}
	if got := groupNow(t, proxyMap, "Other"); got != "🇯🇵 Tokyo 01" {
		t.Errorf("Other = %q, want its group left alone", got)
	}

	// Precise GROUP=PATTERN specs run after bare patterns and win.
	if err := Apply(cfg, []string{"Google=tokyo", "hy2 us"}); err != nil {
		t.Fatalf("Apply with precise group: %v", err)
	}
	if got := groupNow(t, proxyMap, "Google"); got != "🇯🇵 Tokyo 01" {
		t.Errorf("Google = %q, want the precise group selection to win", got)
	}
	if got := groupNow(t, proxyMap, "Media"); got != "🇺🇸 [Hy2]US 01" {
		t.Errorf("Media = %q, want the bare pattern to remain in effect", got)
	}

	err := Apply(cfg, []string{"no-such-node"})
	if err == nil {
		t.Fatal("Apply succeeded without a matching candidate")
	}
	for _, want := range []string{"any select group", "closest candidates", "🇺🇸 [Hy2]US 01", "🇯🇵 Tokyo 01"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	for _, unwanted := range []string{"primary group", "DIRECT", "REJECT", "Proxies,", "Google,"} {
		if strings.Contains(err.Error(), unwanted) {
			t.Errorf("error %q mentions non-node candidate %q", err, unwanted)
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
		"DIRECT":     adapter.NewProxy(outbound.NewDirect()),
		"REJECT":     adapter.NewProxy(outbound.NewReject()),
	}
	allProxies := []string{"COMPATIBLE", "DIRECT", "REJECT"}
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
