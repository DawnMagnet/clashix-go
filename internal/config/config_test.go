package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParsePreservesUnknownKeys(t *testing.T) {
	data := []byte(`
port: 7890
unknown-top: keep
nested:
  future-key: true
  list:
    - one
    - two
proxies:
  - name: node
    type: ss
`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := cfg["unknown-top"]; got != "keep" {
		t.Errorf("unknown-top = %v, want keep", got)
	}
	nested, ok := cfg["nested"].(map[string]any)
	if !ok || nested["future-key"] != true {
		t.Errorf("nested unknown key was not preserved: %#v", cfg["nested"])
	}

	encoded, err := Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	roundTrip, err := Parse(encoded)
	if err != nil {
		t.Fatalf("Parse round trip: %v", err)
	}
	if roundTrip["unknown-top"] != "keep" {
		t.Errorf("unknown key lost after round trip: %#v", roundTrip)
	}
}

func TestParseRejectsNonMapping(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"sequence", "- one\n- two\n"},
		{"scalar", "just a string\n"},
		{"empty", " \n\t"},
		{"invalid", "key: [unterminated\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Parse([]byte(test.data)); err == nil {
				t.Fatal("Parse succeeded, want error")
			}
		})
	}
}

func TestMergeSubscriptions(t *testing.T) {
	first := map[string]any{
		"port": 7890,
		"proxies": []any{
			map[string]any{"name": "first"},
		},
		"proxy-groups": []any{map[string]any{"name": "group"}},
	}
	second := map[string]any{
		"port":    9999,
		"mode":    "global",
		"proxies": []any{map[string]any{"name": "second"}},
	}
	third := map[string]any{
		"proxies": []any{map[string]any{"name": "third"}},
	}

	got := MergeSubscriptions(first, second, third)
	if got["port"] != 7890 {
		t.Errorf("port = %v, want first document value 7890", got["port"])
	}
	if _, ok := got["mode"]; ok {
		t.Error("later document leaked non-proxies key into result")
	}
	proxies, ok := got["proxies"].([]any)
	if !ok || len(proxies) != 3 {
		t.Fatalf("proxies = %#v, want 3 entries", got["proxies"])
	}
	for i, want := range []string{"first", "second", "third"} {
		proxy := proxies[i].(map[string]any)
		if proxy["name"] != want {
			t.Errorf("proxies[%d].name = %v, want %s", i, proxy["name"], want)
		}
	}
	if len(first["proxies"].([]any)) != 1 {
		t.Error("MergeSubscriptions mutated the first document")
	}
}

func TestMergeSubscriptionsEmpty(t *testing.T) {
	got := MergeSubscriptions()
	if got == nil || len(got) != 0 {
		t.Fatalf("MergeSubscriptions() = %#v, want empty map", got)
	}
}

func TestApplyOverlay(t *testing.T) {
	cfg := map[string]any{
		"port":    1,
		"unknown": "keep",
	}
	overlay := Overlay{
		Port:               IntPtr(2),
		MixedPort:          IntPtr(3),
		AllowLAN:           BoolPtr(true),
		Mode:               StringPtr("rule"),
		LogLevel:           StringPtr("debug"),
		ExternalController: StringPtr("127.0.0.1:9090"),
		Secret:             StringPtr("s3cret"),
	}
	ApplyOverlay(cfg, overlay)

	want := map[string]any{
		"port":                2,
		"mixed-port":          3,
		"allow-lan":           true,
		"mode":                "rule",
		"log-level":           "debug",
		"external-controller": "127.0.0.1:9090",
		"secret":              "s3cret",
		"unknown":             "keep",
	}
	for key, value := range want {
		if cfg[key] != value {
			t.Errorf("%s = %v, want %v", key, cfg[key], value)
		}
	}
	if _, ok := cfg["socks-port"]; ok {
		t.Error("nil overlay field changed socks-port")
	}
}

func TestRedact(t *testing.T) {
	cfg := map[string]any{
		"secret": "controller-secret",
		"proxies": []any{
			map[string]any{
				"name":           "node",
				"username":       "alice",
				"password":       "hunter2",
				"auth-str":       "hysteria-secret",
				"pre-shared-key": "wireguard-secret",
				"nested":         map[string]any{"token": "abc", "safe": "value"},
			},
		},
		"proxy-providers": map[string]any{
			"remote": map[string]any{
				"url": "https://provider-user:provider-pass@example.test/private/token?token=query-secret",
				"headers": map[string]any{
					"Authorization": "Bearer header-secret",
				},
			},
		},
	}
	got := Redact(cfg)
	if got["secret"] != Redacted {
		t.Errorf("secret = %v, want %s", got["secret"], Redacted)
	}
	proxy := got["proxies"].([]any)[0].(map[string]any)
	if proxy["username"] != Redacted || proxy["password"] != Redacted {
		t.Errorf("proxy credentials were not redacted: %#v", proxy)
	}
	if proxy["auth-str"] != Redacted || proxy["pre-shared-key"] != Redacted {
		t.Errorf("proxy credential fields were not redacted: %#v", proxy)
	}
	nested := proxy["nested"].(map[string]any)
	if nested["token"] != Redacted || nested["safe"] != "value" {
		t.Errorf("nested redaction = %#v", nested)
	}
	provider := got["proxy-providers"].(map[string]any)["remote"].(map[string]any)
	providerURL := provider["url"].(string)
	for _, secret := range []string{"provider-user", "provider-pass", "private", "token", "query-secret"} {
		if strings.Contains(providerURL, secret) {
			t.Errorf("provider URL leaked %q: %s", secret, providerURL)
		}
	}
	if !strings.Contains(providerURL, "example.test") {
		t.Errorf("provider URL lost its host: %s", providerURL)
	}
	headers := provider["headers"].(map[string]any)
	if headers["Authorization"] != Redacted {
		t.Errorf("authorization header was not redacted: %#v", headers)
	}
	if cfg["secret"] != "controller-secret" {
		t.Error("Redact mutated the input config")
	}
}

func TestSecret(t *testing.T) {
	first, err := GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	second, err := GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	if first == "" || len(first) < 24 {
		t.Errorf("secret %q is too short", first)
	}
	if first == second {
		t.Error("two generated secrets were identical")
	}

	cfg := map[string]any{}
	generated, err := EnsureSecret(cfg)
	if err != nil || !generated {
		t.Fatalf("EnsureSecret = %v, %v; want generated", generated, err)
	}
	before := cfg["secret"]
	generated, err = EnsureSecret(cfg)
	if err != nil || generated {
		t.Fatalf("EnsureSecret second call = %v, %v; want unchanged", generated, err)
	}
	if cfg["secret"] != before {
		t.Error("EnsureSecret replaced an existing secret")
	}
}

func TestWriteAtomicAndLastGood(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	first := map[string]any{"port": 7890, "secret": "first"}
	second := map[string]any{"port": 7891, "secret": "second"}

	if err := Write(path, first); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if _, err := os.Stat(LastGoodPath(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("last-good exists after first write: %v", err)
	}
	if err := Write(path, second); err != nil {
		t.Fatalf("second Write: %v", err)
	}

	active, err := Load(path)
	if err != nil {
		t.Fatalf("Load active: %v", err)
	}
	if active["port"] != 7891 {
		t.Errorf("active port = %v, want 7891", active["port"])
	}
	lastGood, err := Load(LastGoodPath(path))
	if err != nil {
		t.Fatalf("Load last-good: %v", err)
	}
	if lastGood["port"] != 7890 || lastGood["secret"] != "first" {
		t.Errorf("last-good = %#v, want first config", lastGood)
	}

	if runtime.GOOS != "windows" {
		for _, file := range []string{path, LastGoodPath(path)} {
			info, err := os.Stat(file)
			if err != nil {
				t.Fatalf("stat %s: %v", file, err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Errorf("%s mode = %o, want 600", file, info.Mode().Perm())
			}
		}
	}

	invalid := filepath.Join(dir, "invalid.yaml")
	if err := WriteYAML(invalid, []byte("not: [valid")); err == nil {
		t.Fatal("WriteYAML accepted invalid YAML")
	}
	if _, err := os.Stat(invalid); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("invalid write created %s", invalid)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Errorf("temporary file left behind: %s", entry.Name())
		}
	}
}

func TestSubscriptionFetch(t *testing.T) {
	plain := "port: 7890\nproxies:\n  - name: node\n    type: ss\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(plain))
	wrapped := encoded[:20] + "\n" + encoded[20:]
	var proxyHits atomic.Int32

	tests := []struct {
		name        string
		body        string
		status      int
		maxSize     int64
		timeout     time.Duration
		useProxy    bool
		wantErr     bool
		wantPort    any
		wantProxies int
	}{
		{name: "plain YAML", body: plain, status: http.StatusOK, wantPort: 7890, wantProxies: 1},
		{name: "base64", body: encoded, status: http.StatusOK, wantPort: 7890, wantProxies: 1},
		{name: "base64 with whitespace", body: wrapped, status: http.StatusOK, wantPort: 7890, wantProxies: 1},
		{name: "non mapping", body: "- one\n- two\n", status: http.StatusOK, wantErr: true},
		{name: "invalid base64", body: "{{{{not base64", status: http.StatusOK, wantErr: true},
		{name: "http error", body: "denied", status: http.StatusForbidden, wantErr: true},
		{name: "too large", body: plain, status: http.StatusOK, maxSize: 8, wantErr: true},
		{name: "timeout", body: plain, status: http.StatusOK, timeout: 10 * time.Millisecond, wantErr: true},
		{name: "ignores proxy environment", body: plain, status: http.StatusOK, useProxy: true, wantPort: 7890, wantProxies: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var proxyURL string
			if test.useProxy {
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					proxyHits.Add(1)
					http.Error(w, "proxy should not be used", http.StatusBadGateway)
				}))
				defer proxy.Close()
				proxyURL = proxy.URL
				t.Setenv("HTTP_PROXY", proxyURL)
				t.Setenv("HTTPS_PROXY", proxyURL)
				t.Setenv("http_proxy", proxyURL)
				t.Setenv("https_proxy", proxyURL)
				t.Setenv("NO_PROXY", "")
				t.Setenv("no_proxy", "")
			}

			seenUA := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seenUA <- r.Header.Get("User-Agent")
				if test.timeout > 0 {
					time.Sleep(test.timeout * 3)
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()

			fetcher := NewFetcher()
			if test.maxSize > 0 {
				fetcher.MaxSize = test.maxSize
			}
			if test.timeout > 0 {
				fetcher.Client = NewHTTPClient(test.timeout)
			}
			cfg, err := fetcher.Fetch(t.Context(), server.URL)
			if got := <-seenUA; got != SubscriptionUserAgent {
				t.Errorf("User-Agent = %q, want %q", got, SubscriptionUserAgent)
			}
			if test.useProxy && proxyHits.Load() != 0 {
				t.Errorf("proxy was used %d times", proxyHits.Load())
			}
			if test.wantErr {
				if err == nil {
					t.Fatalf("Fetch succeeded, want error (cfg=%#v)", cfg)
				}
				return
			}
			if err != nil {
				t.Fatalf("Fetch: %v", err)
			}
			if cfg["port"] != test.wantPort {
				t.Errorf("port = %v, want %v", cfg["port"], test.wantPort)
			}
			proxies, ok := cfg["proxies"].([]any)
			if !ok || len(proxies) != test.wantProxies {
				t.Errorf("proxies = %#v, want %d", cfg["proxies"], test.wantProxies)
			}
		})
	}
}

func TestFetchRejectsBadURL(t *testing.T) {
	fetcher := NewFetcher()
	if _, err := fetcher.Fetch(t.Context(), "://bad-url"); err == nil {
		t.Fatal("Fetch accepted an invalid URL")
	}
}

func TestWriteYAMLRejectsInvalidAndKeepsActive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := Write(path, map[string]any{"port": 1}); err != nil {
		t.Fatal(err)
	}
	if err := WriteYAML(path, []byte("port: [broken")); err == nil {
		t.Fatal("WriteYAML accepted invalid YAML")
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load after rejected write: %v", err)
	}
	if cfg["port"] != 1 {
		t.Errorf("active config changed after rejected write: %#v", cfg)
	}
}

func TestApplyGeoXDefaults(t *testing.T) {
	t.Run("adds defaults", func(t *testing.T) {
		cfg := map[string]any{}
		ApplyGeoXDefaults(cfg)
		urls, ok := cfg["geox-url"].(map[string]any)
		if !ok {
			t.Fatalf("geox-url = %#v, want mapping", cfg["geox-url"])
		}
		if urls["geoip"] != DefaultGeoXURL["geoip"] {
			t.Errorf("geoip = %v", urls["geoip"])
		}
		if urls["mmdb"] != DefaultGeoXURL["mmdb"] {
			t.Errorf("mmdb = %v", urls["mmdb"])
		}
	})

	t.Run("preserves user value", func(t *testing.T) {
		user := map[string]any{"geoip": "https://example.test/geoip.dat"}
		cfg := map[string]any{"geox-url": user}
		ApplyGeoXDefaults(cfg)
		if cfg["geox-url"].(map[string]any)["geoip"] != "https://example.test/geoip.dat" {
			t.Error("user geox-url was overwritten")
		}
		if len(cfg["geox-url"].(map[string]any)) != 1 {
			t.Error("user geox-url gained default keys")
		}
	})
}

func TestHasProxyCredentials(t *testing.T) {
	tests := []struct {
		name             string
		cfg              map[string]any
		subscriptionUsed bool
		want             bool
	}{
		{name: "empty", cfg: map[string]any{}},
		{name: "subscription used", cfg: map[string]any{}, subscriptionUsed: true, want: true},
		{
			name: "proxies present",
			cfg:  map[string]any{"proxies": []any{map[string]any{"name": "node"}}},
			want: true,
		},
		{
			name: "provider with URL",
			cfg: map[string]any{
				"proxy-providers": map[string]any{
					"remote": map[string]any{"type": "http", "url": "https://example.test/sub"},
				},
			},
			want: true,
		},
		{
			name: "provider without URL",
			cfg: map[string]any{
				"proxy-providers": map[string]any{
					"local": map[string]any{"type": "file", "path": "providers/local.yaml"},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := HasProxyCredentials(test.cfg, test.subscriptionUsed); got != test.want {
				t.Errorf("HasProxyCredentials = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSafeSubscriptionSource(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "credentials and query",
			raw:  "https://user:pass@example.test/private/path?token=secret#fragment",
			want: "https://example.test/...",
		},
		{
			name: "no path",
			raw:  "https://example.test",
			want: "https://example.test/...",
		},
		{
			name: "invalid",
			raw:  "://bad",
			want: "<invalid subscription URL>",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := safeSubscriptionSource(test.raw)
			if got != test.want {
				t.Errorf("safeSubscriptionSource = %q, want %q", got, test.want)
			}
			for _, secret := range []string{"user", "pass", "private", "token", "secret"} {
				if strings.Contains(got, secret) {
					t.Errorf("source leaked %q: %s", secret, got)
				}
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestFetchErrorSanitizesSource(t *testing.T) {
	raw := "https://user:pass@subscription.example.test/private/path?token=query-secret"
	fetcher := NewFetcher()
	fetcher.Client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("request to %s failed", req.URL.String())
		}),
	}
	_, err := fetcher.Fetch(t.Context(), raw)
	if err == nil {
		t.Fatal("Fetch succeeded, want error")
	}
	message := err.Error()
	for _, secret := range []string{"user:pass", "private", "query-secret"} {
		if strings.Contains(message, secret) {
			t.Errorf("fetch error leaked %q: %s", secret, message)
		}
	}
	if !strings.Contains(message, "subscription.example.test") {
		t.Errorf("fetch error lost its sanitized source: %s", message)
	}
}

func TestMergeOverride(t *testing.T) {
	base := map[string]any{
		"port": 7890,
		"dns": map[string]any{
			"enable": true,
			"nameserver": []any{
				"198.51.100.1",
			},
			"nested": map[string]any{"keep": "yes"},
		},
		"proxies": []any{map[string]any{"name": "base-node"}},
		"mode":    "rule",
	}
	override := map[string]any{
		"port": 7891,
		"dns": map[string]any{
			"nameserver": []any{"203.0.113.1", "203.0.113.2"},
			"nested":     map[string]any{"add": "yes"},
		},
		"proxies": []any{map[string]any{"name": "override-node"}},
	}
	got := MergeOverride(base, override)
	if got["port"] != 7891 {
		t.Errorf("port = %v, want override scalar", got["port"])
	}
	if got["mode"] != "rule" {
		t.Errorf("mode = %v, want base scalar", got["mode"])
	}
	dns := got["dns"].(map[string]any)
	if dns["enable"] != true {
		t.Errorf("nested base key was lost: %#v", dns)
	}
	nameservers, ok := dns["nameserver"].([]any)
	if !ok || len(nameservers) != 2 || nameservers[0] != "203.0.113.1" {
		t.Errorf("nameserver = %#v, want replacement list", dns["nameserver"])
	}
	nested := dns["nested"].(map[string]any)
	if nested["keep"] != "yes" || nested["add"] != "yes" {
		t.Errorf("nested maps did not merge: %#v", nested)
	}
	proxies := got["proxies"].([]any)
	if len(proxies) != 1 || proxies[0].(map[string]any)["name"] != "override-node" {
		t.Errorf("proxies = %#v, want replacement list", proxies)
	}
	if len(base["proxies"].([]any)) != 1 {
		t.Error("MergeOverride mutated the base document")
	}
}

func TestSelectSpecs(t *testing.T) {
	cfg := map[string]any{SelectKey: []any{" node-a ", "", "Group=node-b"}}
	specs := SelectSpecs(cfg)
	if len(specs) != 2 || specs[0] != "node-a" || specs[1] != "Group=node-b" {
		t.Fatalf("SelectSpecs = %#v", specs)
	}
	SetSelectSpecs(cfg, []string{"re:^node-", "Other=node"})
	stored := cfg[SelectKey].([]any)
	if len(stored) != 2 || stored[0] != "re:^node-" {
		t.Errorf("SetSelectSpecs = %#v", stored)
	}
	SetSelectSpecs(cfg, nil)
	if _, exists := cfg[SelectKey]; exists {
		t.Error("empty specs did not remove the key")
	}
	cfg[SelectKey] = "single"
	if specs := SelectSpecs(cfg); len(specs) != 1 || specs[0] != "single" {
		t.Errorf("scalar SelectSpecs = %#v", specs)
	}
}
