package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DawnMagnet/clashix-go/internal/config"
	"github.com/DawnMagnet/clashix-go/internal/core"
	"github.com/DawnMagnet/clashix-go/internal/hostnet"
	"github.com/DawnMagnet/clashix-go/internal/payload"
	"github.com/spf13/cobra"
)

var lastFakeCore *fakeCore

type fakeCore struct {
	listener net.Listener
	config   map[string]any
	apply    func([]byte) error
}

func (f *fakeCore) Start(_ context.Context, _ string, cfg []byte) error {
	parsed, err := config.Parse(cfg)
	if err != nil {
		return err
	}
	f.config = parsed
	port, _ := parsed["mixed-port"].(int)
	if port == 0 {
		return nil
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	f.listener = listener
	return nil
}

func (f *fakeCore) Stop() error {
	if f.listener == nil {
		return nil
	}
	err := f.listener.Close()
	f.listener = nil
	return err
}

func (f *fakeCore) Apply(_ string, cfg []byte) error {
	if f.apply != nil {
		return f.apply(cfg)
	}
	return nil
}

func useFakeCore(t *testing.T) *fakeCore {
	t.Helper()
	oldCore, oldValidate := newCore, validateConfig
	fake := &fakeCore{}
	newCore = func() core.Core {
		lastFakeCore = fake
		return fake
	}
	validateConfig = func(string, []byte) error { return nil }
	t.Cleanup(func() {
		newCore = oldCore
		validateConfig = oldValidate
		lastFakeCore = nil
	})
	return fake
}

func writeConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "mixed-port: 0\nmode: rule\nproxies: []\nrules:\n  - MATCH,DIRECT\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVersionCommand(t *testing.T) {
	root := NewRoot("test-version")
	var out strings.Builder
	root.SetOut(&out)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "cx test-version") {
		t.Errorf("version output = %q", output)
	}
	if !strings.Contains(output, "payload: none") {
		t.Errorf("version output does not report payload: %q", output)
	}
}

func TestEffectiveVersionKeepsInjectedVersion(t *testing.T) {
	if got := effectiveVersion("v1.2.3"); got != "v1.2.3" {
		t.Errorf("effectiveVersion = %q, want the injected version", got)
	}
}

func TestBoolFlagHelpShowsExplicitFalse(t *testing.T) {
	root := NewRoot("test-version")
	serve, _, err := root.Find([]string{"serve"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"allow-lan", "ui"} {
		flag := serve.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("--%s is missing", name)
		}
		if !strings.Contains(flag.Usage, "--"+name+"=false") {
			t.Errorf("--%s help = %q, want the explicit false form", name, flag.Usage)
		}
	}
}

func TestNoConfigSourceError(t *testing.T) {
	opts := &configOptions{}
	_, err := loadEffectiveConfig(context.Background(), opts, config.Overlay{}, false, filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("loadEffectiveConfig succeeded without a config source")
	}
	for _, want := range []string{"no configuration source", "--config", "--sub", "cx sub update"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestConfigShowRedactsSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `port: 7890
secret: super-controller-secret
unknown-key: preserved
proxies:
  - name: node
    type: ss
    username: alice
    password: hunter2
proxy-providers:
  remote:
    type: http
    url: https://provider-user:provider-pass@example.test/sub
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	root := NewRoot("dev")
	var out strings.Builder
	root.SetOut(&out)
	root.SetArgs([]string{"config", "show", "--config", path, "--dir", t.TempDir()})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	output := out.String()
	for _, secret := range []string{"super-controller-secret", "hunter2", "provider-user", "provider-pass"} {
		if strings.Contains(output, secret) {
			t.Errorf("config show leaked %q:\n%s", secret, output)
		}
	}
	if !strings.Contains(output, "username: alice") {
		t.Errorf("config show over-redacted the non-secret username:\n%s", output)
	}
	if !strings.Contains(output, config.Redacted) {
		t.Errorf("config show did not mark redacted values:\n%s", output)
	}
	if !strings.Contains(output, "unknown-key: preserved") {
		t.Errorf("config show dropped unknown keys:\n%s", output)
	}
}

func TestRunPropagatesExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell exit test uses sh")
	}
	useFakeCore(t)
	root := NewRoot("dev")
	root.SetArgs([]string{"run", "--config", writeConfig(t), "--", "sh", "-c", "exit 7"})
	err := root.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("Execute error = %v, want ExitError", err)
	}
	if exitErr.Code != 7 {
		t.Errorf("exit code = %d, want 7", exitErr.Code)
	}
}

func TestRunOverridesPortsAndController(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test uses sh")
	}
	useFakeCore(t)
	root := NewRoot("dev")
	root.SetArgs([]string{"run", "--config", writeConfig(t), "--", "sh", "-c", "exit 0"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if lastFakeCore == nil || lastFakeCore.config == nil {
		t.Fatal("core did not receive a configuration")
	}
	cfg := lastFakeCore.config
	if cfg["port"] != 0 {
		t.Errorf("port = %v, want 0", cfg["port"])
	}
	if cfg["socks-port"] != 0 {
		t.Errorf("socks-port = %v, want 0", cfg["socks-port"])
	}
	if cfg["external-controller"] != "" {
		t.Errorf("external-controller = %v, want disabled", cfg["external-controller"])
	}
	if cfg["log-level"] != "warning" {
		t.Errorf("log-level = %v, want warning", cfg["log-level"])
	}
	if port, _ := cfg["mixed-port"].(int); port <= 0 {
		t.Errorf("mixed-port = %v, want a selected port", cfg["mixed-port"])
	}
	if _, exists := cfg["secret"]; exists {
		t.Errorf("run added a secret while the controller is disabled: %#v", cfg["secret"])
	}
}

func TestRunExportsProxyEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell environment test uses sh")
	}
	useFakeCore(t)
	root := NewRoot("dev")
	var out strings.Builder
	root.SetOut(&out)
	root.SetArgs([]string{"run", "--config", writeConfig(t), "--", "sh", "-c", `printf '%s\n' "$http_proxy" "$HTTP_PROXY" "$https_proxy" "$all_proxy" "$no_proxy"`})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("child output = %q, want 5 lines", out.String())
	}
	for _, line := range lines[:3] {
		if !strings.HasPrefix(line, "http://127.0.0.1:") {
			t.Errorf("HTTP proxy = %q", line)
		}
	}
	if !strings.HasPrefix(lines[3], "socks5h://127.0.0.1:") {
		t.Errorf("all_proxy = %q", lines[3])
	}
	if lines[4] != "localhost,127.0.0.1,::1" {
		t.Errorf("no_proxy = %q", lines[4])
	}
}

func TestPackAndReadPayload(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("fake-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "input.yaml")
	if err := os.WriteFile(configPath, []byte("port: 7890\nsecret: pack-secret\nproxies:\n  - name: node\n    type: ss\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	geodataDir := filepath.Join(dir, "geo")
	if err := os.Mkdir(geodataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(geodataDir, "geoip.dat"), []byte("geo"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(geodataDir, "README.txt"), []byte("ignore"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "packed")

	root := NewRoot("dev")
	var stdout, stderr strings.Builder
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"pack", "-o", output,
		"--target-binary", target,
		"--config", configPath,
		"--geodata", geodataDir,
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stderr.String(), "WARNING") {
		t.Error("pack did not print a credential warning")
	}
	files, ok, err := payload.Read(output)
	if err != nil || !ok {
		t.Fatalf("Read packed = %v, %v; want ok", ok, err)
	}
	if string(files["geoip.dat"]) != "geo" {
		t.Errorf("geoip.dat = %q", files["geoip.dat"])
	}
	if _, exists := files["README.txt"]; exists {
		t.Error("pack included an unknown geodata file")
	}
	names, payloadPresent, err := payloadFilesFrom(output)
	if err != nil || !payloadPresent {
		t.Fatalf("payloadFilesFrom = %v, %v; want payload", payloadPresent, err)
	}
	if !strings.Contains(strings.Join(names, ","), "config.yaml") || !strings.Contains(strings.Join(names, ","), "geoip.dat") {
		t.Errorf("payload files = %v", names)
	}
	packedCfg, embedded, err := embeddedConfigFrom(output)
	if err != nil || !embedded {
		t.Fatalf("embeddedConfigFrom = %v, %v; want embedded", embedded, err)
	}
	if packedCfg["port"] != 7890 {
		t.Errorf("packed port = %v", packedCfg["port"])
	}
	if _, exists := packedCfg["secret"]; exists {
		t.Error("packed config retained the controller secret")
	}
	if !strings.Contains(stderr.String(), "stripped the controller secret") {
		t.Errorf("pack did not report stripping the secret: %q", stderr.String())
	}
}

func TestSubUpdateWritesStateConfig(t *testing.T) {
	useFakeCore(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("port: 7890\nproxies:\n  - name: node\n    type: ss\n"))
	}))
	defer server.Close()

	state := t.TempDir()
	root := NewRoot("dev")
	var out strings.Builder
	root.SetOut(&out)
	root.SetArgs([]string{"sub", "update", "--dir", state, "--sub", server.URL})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	path := config.DefaultConfigPath(state)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load cached config: %v", err)
	}
	proxies, ok := cfg["proxies"].([]any)
	if !ok || len(proxies) != 1 {
		t.Fatalf("proxies = %#v, want one", cfg["proxies"])
	}
	if strings.TrimSpace(out.String()) != path {
		t.Errorf("sub update output = %q, want %q", out.String(), path)
	}
}

func TestServeWritesConfigAndStops(t *testing.T) {
	useFakeCore(t)
	state := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	root := NewRoot("dev")
	root.SetContext(ctx)
	root.SetArgs([]string{"serve", "--config", writeConfig(t), "--dir", state})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	cfg, err := config.Load(config.DefaultConfigPath(state))
	if err != nil {
		t.Fatalf("Load served config: %v", err)
	}
	if _, ok := cfg["secret"]; ok {
		t.Errorf("serve added a secret while the controller is disabled: %#v", cfg["secret"])
	}
}

func TestServeUsesCacheWhenSubscriptionHangs(t *testing.T) {
	useFakeCore(t)
	state := t.TempDir()
	cache := map[string]any{
		"mixed-port": 0,
		"mode":       "rule",
		"proxies":    []any{},
		"rules":      []any{"MATCH,DIRECT"},
	}
	if err := config.Write(config.DefaultConfigPath(state), cache); err != nil {
		t.Fatal(err)
	}

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()
	var stderr strings.Builder
	root := NewRoot("dev")
	root.SetErr(&stderr)
	root.SetContext(ctx)
	root.SetArgs([]string{"serve", "--dir", state, "--sub", server.URL})
	started := time.Now()
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("serve took %s with a hanging subscription", elapsed)
	}
	if lastFakeCore == nil || lastFakeCore.config["mixed-port"] != 0 {
		t.Fatalf("core did not start from the cached config: %#v", lastFakeCore)
	}
	if !strings.Contains(stderr.String(), "using cached configuration") {
		t.Errorf("serve did not report the cached config: %q", stderr.String())
	}
}

func TestServeUsesLastGoodWhenActiveConfigInvalid(t *testing.T) {
	useFakeCore(t)
	state := t.TempDir()
	path := config.DefaultConfigPath(state)
	lastGood := []byte("mixed-port: 0\nmode: rule\nproxies: []\nrules:\n  - MATCH,DIRECT\n")
	if err := os.WriteFile(config.LastGoodPath(path), lastGood, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not: [valid"), 0o600); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	var stderr strings.Builder
	root := NewRoot("dev")
	root.SetErr(&stderr)
	root.SetContext(ctx)
	root.SetArgs([]string{"serve", "--dir", state, "--sub", server.URL})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if lastFakeCore == nil {
		t.Fatal("core did not start from last-good")
	}
	if !strings.Contains(stderr.String(), "using cached configuration") || !strings.Contains(stderr.String(), "last-good") {
		t.Errorf("serve did not report last-good fallback: %q", stderr.String())
	}
}

func TestServeCachedConfigRefreshesInBackground(t *testing.T) {
	fake := useFakeCore(t)
	state := t.TempDir()
	cache := map[string]any{"mixed-port": 0, "mode": "rule", "proxies": []any{}, "rules": []any{"MATCH,DIRECT"}}
	if err := config.Write(config.DefaultConfigPath(state), cache); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("mixed-port: 0\nmode: rule\nproxies: []\nrules:\n  - MATCH,DIRECT\nx-refreshed: true\n"))
	}))
	defer server.Close()

	applied := make(chan map[string]any, 1)
	fake.apply = func(data []byte) error {
		cfg, err := config.Parse(data)
		if err != nil {
			return err
		}
		applied <- cfg
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := NewRoot("dev")
	root.SetContext(ctx)
	root.SetArgs([]string{"serve", "--dir", state, "--sub", server.URL})
	done := make(chan error, 1)
	go func() { done <- root.Execute() }()

	select {
	case cfg := <-applied:
		if cfg["x-refreshed"] != true {
			t.Errorf("background refresh applied %#v", cfg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("background subscription refresh was not applied")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestServeCachedConfigSurvivesFailedRefresh(t *testing.T) {
	useFakeCore(t)
	state := t.TempDir()
	cache := map[string]any{"mixed-port": 0, "mode": "rule", "proxies": []any{}, "rules": []any{"MATCH,DIRECT"}}
	if err := config.Write(config.DefaultConfigPath(state), cache); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	var stderr strings.Builder
	root := NewRoot("dev")
	root.SetErr(&stderr)
	root.SetContext(ctx)
	root.SetArgs([]string{"serve", "--dir", state, "--sub", server.URL})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stderr.String(), "configuration active") {
		t.Errorf("serve did not start from cache: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "startup subscription refresh") || !strings.Contains(stderr.String(), "failed") {
		t.Errorf("serve did not warn about the failed refresh: %q", stderr.String())
	}
}

func TestServeUIDisabledDoesNotLogPanel(t *testing.T) {
	useFakeCore(t)
	oldBundled, oldExtract := panelBundled, extractPanel
	panelBundled = func() bool { return true }
	extractPanel = func(dir string) (string, error) {
		target := filepath.Join(dir, "ui")
		if err := os.MkdirAll(target, 0o755); err != nil {
			return "", err
		}
		return target, os.WriteFile(filepath.Join(target, "index.html"), []byte("<html>panel</html>"), 0o644)
	}
	t.Cleanup(func() {
		panelBundled, extractPanel = oldBundled, oldExtract
	})

	state := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := "mixed-port: 0\nexternal-controller: 127.0.0.1:0\nexternal-ui: /tmp/old-panel\nmode: rule\nproxies: []\nrules:\n  - MATCH,DIRECT\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	var stderr strings.Builder
	root := NewRoot("dev")
	root.SetErr(&stderr)
	root.SetContext(ctx)
	root.SetArgs([]string{"serve", "--config", configPath, "--dir", state, "--ui=false"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(stderr.String(), "web panel:") {
		t.Errorf("--ui=false printed a panel URL: %q", stderr.String())
	}
	if lastFakeCore != nil {
		if _, exists := lastFakeCore.config["external-ui"]; exists {
			t.Errorf("--ui=false injected external-ui: %#v", lastFakeCore.config["external-ui"])
		}
	}
}

func TestServeKeepsAutoInterfaceOutOfStateConfig(t *testing.T) {
	useFakeCore(t)
	oldDetect := detectHostTun
	t.Cleanup(func() { detectHostTun = oldDetect })
	if runtime.GOOS == "darwin" {
		detectHostTun = func() (hostnet.Detection, bool) {
			return hostnet.Detection{Tun: "utun0", Physical: "en0"}, true
		}
	} else {
		detectHostTun = func() (hostnet.Detection, bool) {
			return hostnet.Detection{Tun: "Meta", Physical: "eth0"}, true
		}
	}

	state := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	var stderr strings.Builder
	root := NewRoot("dev")
	root.SetErr(&stderr)
	root.SetContext(ctx)
	root.SetArgs([]string{"serve", "--config", writeConfig(t), "--dir", state})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if lastFakeCore == nil {
		t.Fatal("core did not receive a configuration")
	}
	if runtime.GOOS == "darwin" {
		if _, exists := lastFakeCore.config["interface-name"]; exists {
			t.Fatalf("macOS auto-bound the core to %v", lastFakeCore.config["interface-name"])
		}
		if !strings.Contains(stderr.String(), "not auto-binding on macOS") || !strings.Contains(stderr.String(), "--interface en0") {
			t.Errorf("serve did not log the macOS host-TUN hint: %q", stderr.String())
		}
	} else {
		if lastFakeCore.config["interface-name"] != "eth0" {
			t.Fatalf("core config interface-name = %v, want eth0", lastFakeCore.config["interface-name"])
		}
		if !strings.Contains(stderr.String(), "host TUN detected (dev Meta); binding outbound to eth0") {
			t.Errorf("serve did not log the auto interface: %q", stderr.String())
		}
	}
	cfg, err := config.Load(config.DefaultConfigPath(state))
	if err != nil {
		t.Fatalf("Load served config: %v", err)
	}
	if _, exists := cfg["interface-name"]; exists {
		t.Errorf("auto-detected interface-name leaked into the state config: %#v", cfg["interface-name"])
	}
}

func TestServeGeneratesControllerSecret(t *testing.T) {
	useFakeCore(t)
	state := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := "mixed-port: 0\nexternal-controller: 127.0.0.1:0\nmode: rule\nproxies: []\nrules:\n  - MATCH,DIRECT\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	root := NewRoot("dev")
	var stderr strings.Builder
	root.SetErr(&stderr)
	root.SetContext(ctx)
	root.SetArgs([]string{"serve", "--config", configPath, "--dir", state})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	cfg, err := config.Load(config.DefaultConfigPath(state))
	if err != nil {
		t.Fatalf("Load served config: %v", err)
	}
	secret, ok := cfg["secret"].(string)
	if !ok || secret == "" {
		t.Fatalf("serve did not persist a generated secret: %#v", cfg["secret"])
	}
	path := controllerSecretPath(state)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read controller secret file: %v", err)
	}
	if strings.TrimSpace(string(data)) != secret {
		t.Errorf("controller secret file = %q, want persisted secret", data)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("controller secret mode = %o, want 600", info.Mode().Perm())
		}
	}
	if strings.Contains(stderr.String(), secret) {
		t.Errorf("serve leaked the controller secret to stderr: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), path) {
		t.Errorf("serve did not log the controller secret path: %q", stderr.String())
	}

	if _, generated, err := ensureControllerSecret(cfg, secret); err != nil || generated {
		t.Errorf("second startup regenerated the secret: generated=%v err=%v", generated, err)
	}
}

func TestServeReloadTunFailureRollsBack(t *testing.T) {
	fake := useFakeCore(t)
	state := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	calls := 0
	fake.apply = func([]byte) error {
		calls++
		if calls == 1 {
			return fmt.Errorf("%w: test", core.ErrTunNotStarted)
		}
		cancel()
		return nil
	}
	var stderr strings.Builder
	root := NewRoot("dev")
	root.SetErr(&stderr)
	root.SetContext(ctx)
	root.SetArgs([]string{"serve", "--config", writeConfig(t), "--dir", state, "--sub-interval", "20ms"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if calls != 2 {
		t.Fatalf("Apply calls = %d, want reload failure plus rollback", calls)
	}
	for _, want := range []string{"TUN inbound did not start", "rolled back to previous working configuration"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr %q does not mention %q", stderr.String(), want)
		}
	}
	cfg, err := config.Load(config.DefaultConfigPath(state))
	if err != nil {
		t.Fatalf("Load rolled-back config: %v", err)
	}
	if _, exists := cfg["tun"]; exists {
		t.Errorf("rolled-back config retained failed TUN settings: %#v", cfg["tun"])
	}
}

func TestSubUpdateValidationFailureKeepsActive(t *testing.T) {
	useFakeCore(t)
	validateConfig = func(string, []byte) error { return errors.New("invalid config") }

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("port: 7891\nproxies: []\nrules: [MATCH,DIRECT]\n"))
	}))
	defer server.Close()

	state := t.TempDir()
	path := config.DefaultConfigPath(state)
	active := map[string]any{"port": 7890}
	if err := config.Write(path, active); err != nil {
		t.Fatal(err)
	}

	root := NewRoot("dev")
	root.SetArgs([]string{"sub", "update", "--dir", state, "--sub", server.URL})
	if err := root.Execute(); err == nil {
		t.Fatal("sub update succeeded despite validation failure")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg["port"] != 7890 {
		t.Errorf("active config was replaced after validation failure: %#v", cfg)
	}
}

func TestPackWarnsForSubscription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("mixed-port: 0\nmode: rule\nproxies: []\nrules:\n  - MATCH,DIRECT\n"))
	}))
	defer server.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("fake-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "packed")

	root := NewRoot("dev")
	var stdout, stderr strings.Builder
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"pack", "-o", output, "--target-binary", target, "--sub", server.URL})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stderr.String(), "WARNING") || !strings.Contains(stderr.String(), "private") {
		t.Errorf("pack did not warn about subscription-derived credentials: %q", stderr.String())
	}
	if _, ok, err := payload.Read(output); err != nil || !ok {
		t.Fatalf("packed output = %v, %v; want payload", ok, err)
	}
}

func TestPackWarnsWhenTargetAlreadyPacked(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("fake-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := writeConfig(t)
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")

	runPack := func(output string, args ...string) string {
		t.Helper()
		root := NewRoot("dev")
		var stderr strings.Builder
		root.SetErr(&stderr)
		root.SetArgs(append([]string{"pack", "-o", output, "--config", configPath}, args...))
		if err := root.Execute(); err != nil {
			t.Fatalf("pack: %v", err)
		}
		return stderr.String()
	}
	runPack(first, "--target-binary", target)
	stderr := runPack(second, "--target-binary", first)
	if !strings.Contains(stderr, "already contains a payload") {
		t.Errorf("pack did not warn about the existing payload: %q", stderr)
	}
	if _, ok, err := payload.Read(second); err != nil || !ok {
		t.Fatalf("repacked output = %v, %v; want payload", ok, err)
	}
}

func TestEnsureControllerSecret(t *testing.T) {
	tests := []struct {
		name          string
		cfg           map[string]any
		fallback      string
		wantSecret    string
		wantGenerated bool
	}{
		{
			name: "controller disabled does not add a secret",
			cfg:  map[string]any{"external-controller": ""},
		},
		{
			name:          "controller enabled generates one",
			cfg:           map[string]any{"external-controller": "127.0.0.1:9090"},
			wantGenerated: true,
		},
		{
			name:          "TLS controller enabled generates one",
			cfg:           map[string]any{"external-controller-tls": "127.0.0.1:9443"},
			wantGenerated: true,
		},
		{
			name:          "configured secret wins",
			cfg:           map[string]any{"external-controller": "127.0.0.1:9090", "secret": "configured"},
			wantSecret:    "configured",
			wantGenerated: false,
		},
		{
			name:          "reload keeps existing secret",
			cfg:           map[string]any{"external-controller": "127.0.0.1:9090"},
			fallback:      "existing",
			wantSecret:    "existing",
			wantGenerated: false,
		},
		{
			name:       "secret preserved when controller disabled",
			cfg:        map[string]any{"external-controller": "", "secret": "configured"},
			wantSecret: "configured",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			secret, generated, err := ensureControllerSecret(test.cfg, test.fallback)
			if err != nil {
				t.Fatalf("ensureControllerSecret: %v", err)
			}
			if generated != test.wantGenerated {
				t.Errorf("generated = %v, want %v", generated, test.wantGenerated)
			}
			if test.wantSecret != "" && secret != test.wantSecret {
				t.Errorf("secret = %q, want %q", secret, test.wantSecret)
			}
			if test.wantGenerated && secret == "" {
				t.Error("generated secret is empty")
			}
			if test.wantSecret == "" && !test.wantGenerated {
				if _, exists := test.cfg["secret"]; exists {
					t.Errorf("unexpected secret added: %#v", test.cfg["secret"])
				}
			}
		})
	}
}

func TestRunGeneratesSecretWithController(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test uses sh")
	}
	useFakeCore(t)
	root := NewRoot("dev")
	var stderr strings.Builder
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"run", "--config", writeConfig(t),
		"--controller", "127.0.0.1:9090",
		"--", "sh", "-c", "exit 0",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if lastFakeCore == nil || lastFakeCore.config == nil {
		t.Fatal("core did not receive a configuration")
	}
	secret, _ := lastFakeCore.config["secret"].(string)
	if secret == "" {
		t.Fatal("run did not generate a controller secret")
	}
	if strings.Contains(stderr.String(), secret) {
		t.Errorf("run leaked the controller secret to stderr: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "cx run: generated controller secret in ") {
		t.Errorf("run did not log the controller secret path: %q", stderr.String())
	}
}

func TestServeReusesControllerSecretFile(t *testing.T) {
	useFakeCore(t)
	state := t.TempDir()
	const persisted = "persisted-controller-secret"
	if _, err := writeControllerSecret(state, persisted); err != nil {
		t.Fatalf("writeControllerSecret: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := "mixed-port: 0\nexternal-controller: 127.0.0.1:0\nmode: rule\nproxies: []\nrules:\n  - MATCH,DIRECT\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	root := NewRoot("dev")
	var stderr strings.Builder
	root.SetErr(&stderr)
	root.SetContext(ctx)
	root.SetArgs([]string{"serve", "--config", configPath, "--dir", state})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if lastFakeCore == nil || lastFakeCore.config["secret"] != persisted {
		t.Fatalf("serve secret = %#v, want the persisted file value", lastFakeCore.config["secret"])
	}
	if strings.Contains(stderr.String(), persisted) {
		t.Errorf("serve leaked the persisted secret: %q", stderr.String())
	}
}

func TestRunPrintsSecretOnTTYStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test uses sh")
	}
	oldTerminal := stderrIsTerminal
	stderrIsTerminal = func() bool { return true }
	t.Cleanup(func() { stderrIsTerminal = oldTerminal })

	useFakeCore(t)
	root := NewRoot("dev")
	var stderr strings.Builder
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"run", "--config", writeConfig(t),
		"--controller", "127.0.0.1:9090",
		"--", "sh", "-c", "exit 0",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	secret, _ := lastFakeCore.config["secret"].(string)
	if secret == "" {
		t.Fatal("run did not generate a controller secret")
	}
	if !strings.Contains(stderr.String(), "generated controller secret: "+secret) {
		t.Errorf("run did not print the secret on a TTY stderr: %q", stderr.String())
	}
}

func TestRunDisablesAlternateControllers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test uses sh")
	}
	useFakeCore(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	content := `mixed-port: 0
mode: rule
external-controller-tls: 127.0.0.1:9443
external-controller-unix: /tmp/cx.sock
external-controller-pipe: \\.\pipe\cx
proxies: []
rules:
  - MATCH,DIRECT
`
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	root := NewRoot("dev")
	root.SetArgs([]string{"run", "--config", configPath, "--", "sh", "-c", "exit 0"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if lastFakeCore == nil || lastFakeCore.config == nil {
		t.Fatal("core did not receive a configuration")
	}
	for _, key := range []string{"external-controller-tls", "external-controller-unix", "external-controller-pipe"} {
		if _, exists := lastFakeCore.config[key]; exists {
			t.Errorf("run kept alternate controller key %q", key)
		}
	}
}

func TestRunTunFlagOverlay(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test uses sh")
	}
	useFakeCore(t)
	root := NewRoot("dev")
	root.SetArgs([]string{
		"run", "--config", writeConfig(t),
		"--tun", "--tun-stack", "gvisor", "--tun-device", "cx0",
		"--tun-exclude", "10.0.0.0/8", "--tun-exclude", "2001:db8::/32",
		"--", "sh", "-c", "exit 0",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tun, ok := lastFakeCore.config["tun"].(map[string]any)
	if !ok {
		t.Fatalf("tun config = %#v", lastFakeCore.config["tun"])
	}
	if tun["enable"] != true || tun["stack"] != "gvisor" || tun["device"] != "cx0" {
		t.Errorf("tun enable/stack/device = %#v", tun)
	}
	if tun["auto-route"] != true || tun["auto-detect-interface"] != true {
		t.Errorf("tun auto-route/auto-detect-interface = %#v", tun)
	}
	routes, ok := tun["route-exclude-address"].([]any)
	if !ok || len(routes) != 2 || routes[0] != "10.0.0.0/8" || routes[1] != "2001:db8::/32" {
		t.Errorf("route-exclude-address = %#v", tun["route-exclude-address"])
	}
	wantTable, wantRule := deriveTunIndexes("cx0")
	if tun["iproute2-table-index"] != wantTable || tun["iproute2-rule-index"] != wantRule {
		t.Errorf("iproute2 indexes = %v/%v, want %d/%d", tun["iproute2-table-index"], tun["iproute2-rule-index"], wantTable, wantRule)
	}
	if wantTable == 2022 || wantRule == 9000 {
		t.Errorf("derived indexes reused defaults: %d/%d", wantTable, wantRule)
	}
}

func TestTunIndexFlagsAndAutoDerive(t *testing.T) {
	newOptions := func(args ...string) (*cobra.Command, *configOptions) {
		cmd := &cobra.Command{}
		opts := &configOptions{}
		addTunFlags(cmd, opts)
		if err := cmd.ParseFlags(args); err != nil {
			t.Fatal(err)
		}
		if err := opts.validateTun(cmd); err != nil {
			t.Fatal(err)
		}
		return cmd, opts
	}

	cmd, opts := newOptions("--tun", "--tun-device", "cxtun9", "--tun-route-table", "3456", "--tun-rule-index", "7890")
	cfg := map[string]any{}
	config.ApplyOverlay(cfg, opts.overlay(cmd))
	applyTunAutoIndexes(opts, cfg)
	tun := cfg["tun"].(map[string]any)
	if tun["iproute2-table-index"] != 3456 || tun["iproute2-rule-index"] != 7890 {
		t.Errorf("explicit indexes = %#v", tun)
	}

	cmd, opts = newOptions("--tun", "--tun-device", "cxtun9")
	cfg = map[string]any{}
	config.ApplyOverlay(cfg, opts.overlay(cmd))
	applyTunAutoIndexes(opts, cfg)
	tun = cfg["tun"].(map[string]any)
	wantTable, wantRule := deriveTunIndexes("cxtun9")
	if tun["iproute2-table-index"] != wantTable || tun["iproute2-rule-index"] != wantRule {
		t.Errorf("derived indexes = %#v, want %d/%d", tun, wantTable, wantRule)
	}

	cmd, opts = newOptions("--tun", "--tun-device", "cxtun9")
	cfg = map[string]any{"tun": map[string]any{"enable": true, "iproute2-table-index": 2222, "iproute2-rule-index": 7777}}
	config.ApplyOverlay(cfg, opts.overlay(cmd))
	applyTunAutoIndexes(opts, cfg)
	tun = cfg["tun"].(map[string]any)
	if tun["iproute2-table-index"] != 2222 || tun["iproute2-rule-index"] != 7777 {
		t.Errorf("configured indexes were overwritten: %#v", tun)
	}

	cmd, opts = newOptions("--tun", "--tun-device", "Meta")
	cfg = map[string]any{}
	config.ApplyOverlay(cfg, opts.overlay(cmd))
	applyTunAutoIndexes(opts, cfg)
	tun = cfg["tun"].(map[string]any)
	if _, exists := tun["iproute2-table-index"]; exists {
		t.Errorf("default device derived an index: %#v", tun)
	}
}

func TestOutboundFlagsOverlay(t *testing.T) {
	opts := &configOptions{}
	cmd := &cobra.Command{}
	addOutboundFlags(cmd, opts)
	if err := cmd.Flags().Set("interface", " eth0 "); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("routing-mark", "123"); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{"interface-name": "old", "routing-mark": 1}
	config.ApplyOverlay(cfg, opts.overlay(cmd))
	if cfg["interface-name"] != "eth0" || cfg["routing-mark"] != 123 {
		t.Errorf("overlay = %#v, want trimmed interface and routing mark", cfg)
	}

	empty := &configOptions{}
	emptyCmd := &cobra.Command{}
	addOutboundFlags(emptyCmd, empty)
	if err := emptyCmd.Flags().Set("interface", ""); err != nil {
		t.Fatal(err)
	}
	emptyCfg := map[string]any{"interface-name": "old"}
	config.ApplyOverlay(emptyCfg, empty.overlay(emptyCmd))
	if emptyCfg["interface-name"] != "" {
		t.Errorf("--interface= did not clear interface-name: %#v", emptyCfg)
	}
}

func TestApplyAutoInterface(t *testing.T) {
	oldDetect := detectHostTun
	t.Cleanup(func() { detectHostTun = oldDetect })

	calls := 0
	detectHostTun = func() (hostnet.Detection, bool) {
		calls++
		return hostnet.Detection{Tun: "Meta", Physical: "eth0"}, true
	}
	newCommand := func() (*cobra.Command, *configOptions) {
		cmd := &cobra.Command{}
		opts := &configOptions{}
		addOutboundFlags(cmd, opts)
		cmd.SetErr(&strings.Builder{})
		return cmd, opts
	}

	cmd, opts := newCommand()
	cfg := map[string]any{}
	calls = 0
	if !applyAutoInterfaceForGOOS("linux", cmd, "run", opts, cfg) {
		t.Fatal("auto interface was not applied")
	}
	if cfg["interface-name"] != "eth0" {
		t.Errorf("interface-name = %v, want eth0", cfg["interface-name"])
	}
	if !strings.Contains(cmd.ErrOrStderr().(*strings.Builder).String(), "host TUN detected (dev Meta); binding outbound to eth0") {
		t.Errorf("missing auto interface log")
	}

	detectHostTun = func() (hostnet.Detection, bool) {
		calls++
		return hostnet.Detection{Tun: "utun0", Physical: "en0"}, true
	}
	cmd, opts = newCommand()
	cfg = map[string]any{}
	calls = 0
	if applyAutoInterfaceForGOOS("darwin", cmd, "run", opts, cfg) {
		t.Fatal("macOS auto interface was applied")
	}
	if _, exists := cfg["interface-name"]; exists {
		t.Errorf("macOS added interface-name: %v", cfg["interface-name"])
	}
	if msg := cmd.ErrOrStderr().(*strings.Builder).String(); !strings.Contains(msg, "not auto-binding on macOS") || !strings.Contains(msg, "--interface en0") {
		t.Errorf("missing macOS hint: %q", msg)
	}

	tests := []struct {
		name   string
		cfg    map[string]any
		setup  func(*cobra.Command)
		called bool
	}{
		{name: "user interface", cfg: map[string]any{"interface-name": "wlan0"}},
		{name: "user routing mark", cfg: map[string]any{"routing-mark": 99}},
		{name: "tun enabled", cfg: map[string]any{"tun": map[string]any{"enable": true}}},
		{name: "explicit empty interface", cfg: map[string]any{}, setup: func(cmd *cobra.Command) { _ = cmd.Flags().Set("interface", "") }},
		{name: "explicit routing mark", cfg: map[string]any{}, setup: func(cmd *cobra.Command) { _ = cmd.Flags().Set("routing-mark", "0") }},
		{name: "auto disabled", cfg: map[string]any{}, setup: func(cmd *cobra.Command) { _ = cmd.Flags().Set("no-auto-interface", "true") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd, opts := newCommand()
			if test.setup != nil {
				test.setup(cmd)
			}
			calls = 0
			if applyAutoInterfaceForGOOS("linux", cmd, "run", opts, test.cfg) {
				t.Fatal("auto interface unexpectedly applied")
			}
			if calls != 0 {
				t.Errorf("detection ran %d times, want 0", calls)
			}
		})
	}
}

func TestTunFlagsValidateCIDR(t *testing.T) {
	root := NewRoot("dev")
	root.SetArgs([]string{
		"run", "--config", writeConfig(t),
		"--tun", "--tun-exclude", "not-a-cidr",
		"--", "sh", "-c", "exit 0",
	})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "CIDR") {
		t.Fatalf("Execute error = %v, want CIDR validation error", err)
	}
}

func TestTunDeviceRequiresName(t *testing.T) {
	root := NewRoot("dev")
	root.SetArgs([]string{
		"run", "--config", writeConfig(t),
		"--tun", "--tun-device", " ",
		"--", "sh", "-c", "exit 0",
	})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--tun-device") {
		t.Fatalf("Execute error = %v, want --tun-device validation error", err)
	}
}

func TestRunRejectsOccupiedMixedPort(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test uses sh")
	}
	useFakeCore(t)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	root := NewRoot("dev")
	root.SetArgs([]string{
		"run", "--config", writeConfig(t),
		"--mixed-port", strconv.Itoa(port),
		"--", "sh", "-c", "exit 0",
	})
	err = root.Execute()
	if err == nil {
		t.Fatal("run succeeded with an occupied mixed port")
	}
	if !strings.Contains(err.Error(), "already in use") {
		t.Errorf("error = %v, want an already-in-use message", err)
	}
}

func TestRunChildHonorsCanceledContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test uses sh")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	marker := filepath.Join(t.TempDir(), "started")
	root := NewRoot("dev")
	err := runChild(ctx, root, []string{"sh", "-c", "touch " + marker}, 1234)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runChild error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("child ran despite a canceled context: %v", err)
	}
}

func TestServeGeneratesTLSControllerSecret(t *testing.T) {
	useFakeCore(t)
	state := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := "mixed-port: 0\nexternal-controller-tls: 127.0.0.1:0\nmode: rule\nproxies: []\nrules:\n  - MATCH,DIRECT\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	root := NewRoot("dev")
	var stderr strings.Builder
	root.SetErr(&stderr)
	root.SetContext(ctx)
	root.SetArgs([]string{"serve", "--config", configPath, "--dir", state})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	cfg, err := config.Load(config.DefaultConfigPath(state))
	if err != nil {
		t.Fatalf("Load served config: %v", err)
	}
	secret, ok := cfg["secret"].(string)
	if !ok || secret == "" {
		t.Fatalf("serve did not persist a TLS controller secret: %#v", cfg["secret"])
	}
	path := controllerSecretPath(state)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read controller secret file: %v", err)
	}
	if strings.TrimSpace(string(data)) != secret {
		t.Errorf("controller secret file = %q, want persisted secret", data)
	}
	if strings.Contains(stderr.String(), secret) {
		t.Errorf("serve leaked the TLS controller secret to stderr: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), path) {
		t.Errorf("serve did not log the TLS controller secret path: %q", stderr.String())
	}
}

func TestRunUIFlagEnablesController(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test uses sh")
	}
	useFakeCore(t)
	root := NewRoot("dev")
	var stderr strings.Builder
	root.SetErr(&stderr)
	root.SetArgs([]string{"run", "--config", writeConfig(t), "--ui", "--", "sh", "-c", "exit 0"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if lastFakeCore == nil || lastFakeCore.config == nil {
		t.Fatal("core did not receive a configuration")
	}
	if got := lastFakeCore.config["external-controller"]; got != "127.0.0.1:9090" {
		t.Errorf("external-controller = %v, want 127.0.0.1:9090", got)
	}
	if secret, _ := lastFakeCore.config["secret"].(string); secret == "" {
		t.Error("--ui did not generate a controller secret")
	}
	if !panelBundled() && !strings.Contains(stderr.String(), "web panel assets are not bundled") {
		t.Errorf("run did not report an unbundled panel: %q", stderr.String())
	}
}

func TestRunCXUIEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test uses sh")
	}
	useFakeCore(t)
	t.Setenv("CX_UI", "true")
	t.Setenv("CX_UI_ADDR", "127.0.0.1:19090")
	root := NewRoot("dev")
	root.SetArgs([]string{"run", "--config", writeConfig(t), "--", "sh", "-c", "exit 0"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := lastFakeCore.config["external-controller"]; got != "127.0.0.1:19090" {
		t.Errorf("external-controller = %v, want CX_UI_ADDR value", got)
	}
}

func TestRunCXSecretEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test uses sh")
	}
	useFakeCore(t)
	t.Setenv("CX_SECRET", "from-environment")
	root := NewRoot("dev")
	root.SetArgs([]string{"run", "--config", writeConfig(t), "--", "sh", "-c", "exit 0"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := lastFakeCore.config["secret"]; got != "from-environment" {
		t.Errorf("secret = %v, want environment value", got)
	}
}

func TestSetupPanelWithBundledAssets(t *testing.T) {
	oldBundled, oldExtract := panelBundled, extractPanel
	panelBundled = func() bool { return true }
	extractPanel = func(dir string) (string, error) {
		target := filepath.Join(dir, "ui")
		if err := os.MkdirAll(target, 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(target, "index.html"), []byte("<html>panel</html>"), 0o644); err != nil {
			return "", err
		}
		return target, nil
	}
	t.Cleanup(func() {
		panelBundled, extractPanel = oldBundled, oldExtract
	})

	dir := t.TempDir()
	cfg := map[string]any{"external-controller": "127.0.0.1:9090"}
	root := NewRoot("dev")
	var stderr strings.Builder
	root.SetErr(&stderr)
	url, err := setupPanel(dir, cfg, true, true, "serve", root)
	if err != nil {
		t.Fatalf("setupPanel: %v", err)
	}
	if cfg["external-ui"] != filepath.Join(dir, "ui") {
		t.Errorf("external-ui = %v", cfg["external-ui"])
	}
	if url != "http://127.0.0.1:9090/ui/" {
		t.Errorf("panel URL = %q", url)
	}
}

func TestPanelURL(t *testing.T) {
	tests := []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{
			name: "loopback HTTP",
			cfg:  map[string]any{"external-controller": "127.0.0.1:9090"},
			want: "http://127.0.0.1:9090/ui/",
		},
		{
			name: "wildcard HTTP",
			cfg:  map[string]any{"external-controller": "0.0.0.0:9090"},
			want: "http://127.0.0.1:9090/ui/",
		},
		{
			name: "TLS only",
			cfg:  map[string]any{"external-controller-tls": "127.0.0.1:9443"},
			want: "https://127.0.0.1:9443/ui/",
		},
		{
			name: "disabled",
			cfg:  map[string]any{},
			want: "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, _ := panelURL(test.cfg)
			if got != test.want {
				t.Errorf("panelURL = %q, want %q", got, test.want)
			}
		})
	}
}

func TestIsLoopbackAddress(t *testing.T) {
	tests := []struct {
		address string
		want    bool
	}{
		{"127.0.0.1:9090", true},
		{"[::1]:9090", true},
		{"localhost:9090", true},
		{"0.0.0.0:9090", false},
		{":9090", false},
		{"192.0.2.1:9090", false},
		{"example.test:9090", false},
	}
	for _, test := range tests {
		if got := isLoopbackAddress(test.address); got != test.want {
			t.Errorf("isLoopbackAddress(%q) = %v, want %v", test.address, got, test.want)
		}
	}
}

func TestWarnInsecureController(t *testing.T) {
	tests := []struct {
		name     string
		cfg      map[string]any
		wantWarn bool
	}{
		{
			name: "loopback without secret",
			cfg:  map[string]any{"external-controller": "127.0.0.1:9090"},
		},
		{
			name:     "non-loopback without secret",
			cfg:      map[string]any{"external-controller": "0.0.0.0:9090"},
			wantWarn: true,
		},
		{
			name:     "TLS non-loopback without secret",
			cfg:      map[string]any{"external-controller-tls": "192.0.2.1:9443"},
			wantWarn: true,
		},
		{
			name: "non-loopback with secret",
			cfg:  map[string]any{"external-controller": "0.0.0.0:9090", "secret": "configured"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := NewRoot("dev")
			var stderr strings.Builder
			root.SetErr(&stderr)
			warnInsecureController(root, "serve", test.cfg)
			got := strings.Contains(stderr.String(), "WARNING")
			if got != test.wantWarn {
				t.Errorf("warning = %v, want %v (%q)", got, test.wantWarn, stderr.String())
			}
		})
	}
}

func TestRunUIArgsReachChild(t *testing.T) {
	t.Setenv("CX_TEST_CHILD_ARGS", "1")
	tests := []struct {
		name           string
		uiArgs         []string
		wantController string
	}{
		{
			name:           "ui with separate address",
			uiArgs:         []string{"--ui", "--ui-addr", "127.0.0.1:19091"},
			wantController: "127.0.0.1:19091",
		},
		{
			name:           "bare ui",
			uiArgs:         []string{"--ui"},
			wantController: defaultUIAddress,
		},
		{
			name:           "address implies ui",
			uiArgs:         []string{"--ui-addr", "127.0.0.1:19092"},
			wantController: "127.0.0.1:19092",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			useFakeCore(t)
			root := NewRoot("dev")
			var out strings.Builder
			root.SetOut(&out)
			args := []string{"run", "--config", writeConfig(t)}
			args = append(args, test.uiArgs...)
			args = append(args, "--", os.Args[0], "-test.run=TestRunChildArgsHelper", "--", "alpha", "beta")
			root.SetArgs(args)
			if err := root.Execute(); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if lastFakeCore == nil || lastFakeCore.config == nil {
				t.Fatal("core did not receive a configuration")
			}
			if got := lastFakeCore.config["external-controller"]; got != test.wantController {
				t.Errorf("external-controller = %v, want %v", got, test.wantController)
			}
			if got := strings.TrimSpace(out.String()); got != "alpha\nbeta" {
				t.Errorf("child args = %q, want %q", got, "alpha\nbeta")
			}
		})
	}
}

func TestRunChildArgsHelper(t *testing.T) {
	if os.Getenv("CX_TEST_CHILD_ARGS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			fmt.Print(strings.Join(os.Args[i+1:], "\n"))
			os.Exit(0)
		}
	}
	os.Exit(2)
}

func TestResolveUIEnvAndFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		want    string
		wantSet bool
	}{
		{name: "disabled by default"},
		{name: "ui flag", args: []string{"--ui"}, want: defaultUIAddress, wantSet: true},
		{name: "ui addr flag", args: []string{"--ui-addr", "127.0.0.1:19091"}, want: "127.0.0.1:19091", wantSet: true},
		{name: "false ui flag disables env", args: []string{"--ui=false"}, env: map[string]string{"CX_UI": "true"}},
		{name: "bool env", env: map[string]string{"CX_UI": "yes"}, want: defaultUIAddress, wantSet: true},
		{name: "env address", env: map[string]string{"CX_UI": "1", "CX_UI_ADDR": "127.0.0.1:19092"}, want: "127.0.0.1:19092", wantSet: true},
		{name: "address env alone", env: map[string]string{"CX_UI_ADDR": "127.0.0.1:19093"}, want: "127.0.0.1:19093", wantSet: true},
		{name: "false env disables address env", env: map[string]string{"CX_UI": "false", "CX_UI_ADDR": "127.0.0.1:19094"}},
		{name: "legacy address env", env: map[string]string{"CX_UI": "127.0.0.1:19095"}, want: "127.0.0.1:19095", wantSet: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CX_UI", "")
			t.Setenv("CX_UI_ADDR", "")
			for key, value := range test.env {
				t.Setenv(key, value)
			}
			cmd := newRunCmd()
			if err := cmd.ParseFlags(test.args); err != nil {
				t.Fatal(err)
			}
			enabled, err := cmd.Flags().GetBool("ui")
			if err != nil {
				t.Fatal(err)
			}
			address, err := cmd.Flags().GetString("ui-addr")
			if err != nil {
				t.Fatal(err)
			}
			got, gotSet := resolveUI(cmd, enabled, address)
			if got != test.want || gotSet != test.wantSet {
				t.Errorf("resolveUI = %q, %v; want %q, %v", got, gotSet, test.want, test.wantSet)
			}
		})
	}
}

func TestOverridePrecedenceAndDeepMerge(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "base.yaml")
	base := `port: 1
mode: rule
dns:
  enable: true
  nameserver: [198.51.100.1]
proxies: []
rules:
  - MATCH,DIRECT
`
	if err := os.WriteFile(configPath, []byte(base), 0o600); err != nil {
		t.Fatal(err)
	}

	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(state, "override.yaml"), []byte("port: 2\ndns:\n  nameserver: [198.51.100.2]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	flagOverride := filepath.Join(dir, "flag.yaml")
	if err := os.WriteFile(flagOverride, []byte("port: 3\ndns:\n  nested:\n    keep: yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	envOverride := filepath.Join(dir, "env.yaml")
	if err := os.WriteFile(envOverride, []byte("port: 4\ndns:\n  nameserver: [198.51.100.4]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CX_OVERRIDE", envOverride)

	opts := &configOptions{
		configPath:  configPath,
		overrides:   []string{flagOverride},
		overrideDir: state,
	}
	cfg, err := loadEffectiveConfig(context.Background(), opts, config.Overlay{Port: config.IntPtr(5)}, false, "")
	if err != nil {
		t.Fatalf("loadEffectiveConfig: %v", err)
	}
	if cfg["port"] != 5 {
		t.Errorf("port = %v, want CLI flag 5", cfg["port"])
	}
	if cfg["mode"] != "rule" {
		t.Errorf("mode = %v, want base value", cfg["mode"])
	}
	dns, ok := cfg["dns"].(map[string]any)
	if !ok {
		t.Fatalf("dns = %#v, want mapping", cfg["dns"])
	}
	if dns["enable"] != true {
		t.Errorf("dns.enable = %v, want preserved base value", dns["enable"])
	}
	nameservers := dns["nameserver"].([]any)
	if len(nameservers) != 1 || nameservers[0] != "198.51.100.4" {
		t.Errorf("dns.nameserver = %#v, want environment override", nameservers)
	}
	nested := dns["nested"].(map[string]any)
	if nested["keep"] != "yes" {
		t.Errorf("dns.nested = %#v, want flag override merged", nested)
	}
}

func TestRunSelectionPrecedence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test uses sh")
	}
	writeSelectionConfig := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "config.yaml")
		content := `mixed-port: 0
mode: rule
x-cx-select:
  - from-config
proxies: []
proxy-groups: []
rules:
  - MATCH,DIRECT
`
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	t.Run("config default", func(t *testing.T) {
		useFakeCore(t)
		root := NewRoot("dev")
		root.SetArgs([]string{"run", "--config", writeSelectionConfig(t), "--", "sh", "-c", "exit 0"})
		if err := root.Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		specs, _ := lastFakeCore.config[config.SelectKey].([]any)
		if len(specs) != 1 || specs[0] != "from-config" {
			t.Errorf("selection = %#v, want config value", lastFakeCore.config[config.SelectKey])
		}
	})

	t.Run("environment overrides config", func(t *testing.T) {
		useFakeCore(t)
		t.Setenv("CX_SELECT", "env-a;Group=env-b")
		root := NewRoot("dev")
		root.SetArgs([]string{"run", "--config", writeSelectionConfig(t), "--", "sh", "-c", "exit 0"})
		if err := root.Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		specs, _ := lastFakeCore.config[config.SelectKey].([]any)
		if len(specs) != 2 || specs[0] != "env-a" || specs[1] != "Group=env-b" {
			t.Errorf("selection = %#v, want environment specs", lastFakeCore.config[config.SelectKey])
		}
	})

	t.Run("flags override environment", func(t *testing.T) {
		useFakeCore(t)
		t.Setenv("CX_SELECT", "env-a")
		root := NewRoot("dev")
		root.SetArgs([]string{
			"run", "--config", writeSelectionConfig(t),
			"--select", "flag-a", "--select", "Group=flag-b",
			"--", "sh", "-c", "exit 0",
		})
		if err := root.Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		specs, _ := lastFakeCore.config[config.SelectKey].([]any)
		if len(specs) != 2 || specs[0] != "flag-a" || specs[1] != "Group=flag-b" {
			t.Errorf("selection = %#v, want flags", lastFakeCore.config[config.SelectKey])
		}
	})
}

func TestPackEmbedsSelection(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("fake-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := writeConfig(t)
	output := filepath.Join(dir, "packed")

	root := NewRoot("dev")
	root.SetArgs([]string{
		"pack", "-o", output,
		"--target-binary", target,
		"--config", configPath,
		"--select", "Proxy=hy2 us",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	packed, embedded, err := embeddedConfigFrom(output)
	if err != nil || !embedded {
		t.Fatalf("embeddedConfigFrom = %v, %v", embedded, err)
	}
	specs, _ := packed[config.SelectKey].([]any)
	if len(specs) != 1 || specs[0] != "Proxy=hy2 us" {
		t.Errorf("packed selection = %#v", packed[config.SelectKey])
	}
}

func TestSubFileAndEnvironment(t *testing.T) {
	useFakeCore(t)
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("port: 7890\nproxies:\n  - name: file-node\n    type: ss\n"))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("port: 7891\nproxies:\n  - name: env-node\n    type: ss\n"))
	}))
	defer second.Close()
	t.Setenv("CX_SUB", second.URL)

	dir := t.TempDir()
	subFile := filepath.Join(dir, "subs.txt")
	content := "# comment\n\n" + first.URL + "\n"
	if err := os.WriteFile(subFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(subFile, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	state := t.TempDir()
	root := NewRoot("dev")
	var stderr strings.Builder
	root.SetErr(&stderr)
	root.SetArgs([]string{"sub", "update", "--dir", state, "--sub-file", subFile})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	cfg, err := config.Load(config.DefaultConfigPath(state))
	if err != nil {
		t.Fatal(err)
	}
	proxies, ok := cfg["proxies"].([]any)
	if !ok || len(proxies) != 2 {
		t.Fatalf("proxies = %#v, want file and environment subscriptions", cfg["proxies"])
	}
	if proxies[0].(map[string]any)["name"] != "file-node" || proxies[1].(map[string]any)["name"] != "env-node" {
		t.Errorf("proxy order = %#v", proxies)
	}
	if runtime.GOOS != "windows" && !strings.Contains(stderr.String(), "group or world readable") {
		t.Errorf("sub file warning missing: %q", stderr.String())
	}
}

func TestRunLoopbackByDefault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test uses sh")
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := `mixed-port: 0
mode: rule
allow-lan: true
bind-address: 0.0.0.0
proxies: []
rules:
  - MATCH,DIRECT
`
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("default", func(t *testing.T) {
		useFakeCore(t)
		root := NewRoot("dev")
		root.SetArgs([]string{"run", "--config", configPath, "--", "sh", "-c", "exit 0"})
		if err := root.Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if lastFakeCore.config["allow-lan"] != false {
			t.Errorf("allow-lan = %v, want false", lastFakeCore.config["allow-lan"])
		}
		if lastFakeCore.config["bind-address"] != "127.0.0.1" {
			t.Errorf("bind-address = %v, want 127.0.0.1", lastFakeCore.config["bind-address"])
		}
	})

	t.Run("allow-lan flag", func(t *testing.T) {
		useFakeCore(t)
		root := NewRoot("dev")
		root.SetArgs([]string{"run", "--config", configPath, "--allow-lan", "--", "sh", "-c", "exit 0"})
		if err := root.Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if lastFakeCore.config["allow-lan"] != true {
			t.Errorf("allow-lan = %v, want true", lastFakeCore.config["allow-lan"])
		}
		if lastFakeCore.config["bind-address"] != "0.0.0.0" {
			t.Errorf("bind-address = %v, want configured value", lastFakeCore.config["bind-address"])
		}
	})
}

func TestRunMixedPortZeroIsAutoPicked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test uses sh")
	}
	useFakeCore(t)
	root := NewRoot("dev")
	root.SetArgs([]string{"run", "--config", writeConfig(t), "--mixed-port", "0", "--", "sh", "-c", "exit 0"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if port, _ := lastFakeCore.config["mixed-port"].(int); port <= 0 {
		t.Errorf("mixed-port = %v, want an auto-picked port", lastFakeCore.config["mixed-port"])
	}
}

func TestPortZeroDisablesListeners(t *testing.T) {
	root := NewRoot("dev")
	var out strings.Builder
	root.SetOut(&out)
	root.SetArgs([]string{
		"config", "show",
		"--config", writeConfig(t),
		"--dir", t.TempDir(),
		"--port", "0",
		"--socks-port", "0",
		"--mixed-port", "0",
		"--redir-port", "0",
		"--tproxy-port", "0",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, key := range []string{"port", "socks-port", "mixed-port", "redir-port", "tproxy-port"} {
		if !strings.Contains(out.String(), key+": 0") {
			t.Errorf("config show output does not contain %s: 0:\n%s", key, out.String())
		}
	}
}
