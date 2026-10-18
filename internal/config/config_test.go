package config

import (
	"encoding/base64"
	"errors"
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
				"name":     "node",
				"username": "alice",
				"password": "hunter2",
				"nested":   map[string]any{"token": "abc", "safe": "value"},
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
	nested := proxy["nested"].(map[string]any)
	if nested["token"] != Redacted || nested["safe"] != "value" {
		t.Errorf("nested redaction = %#v", nested)
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
