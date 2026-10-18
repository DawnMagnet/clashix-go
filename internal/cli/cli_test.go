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
	"strings"
	"testing"
	"time"

	"github.com/DawnMagnet/clashix-go/internal/config"
	"github.com/DawnMagnet/clashix-go/internal/core"
	"github.com/DawnMagnet/clashix-go/internal/payload"
)

var lastFakeCore *fakeCore

type fakeCore struct {
	listener net.Listener
	config   map[string]any
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

func (f *fakeCore) Apply(string, []byte) error { return nil }

func useFakeCore(t *testing.T) {
	t.Helper()
	oldCore, oldValidate := newCore, validateConfig
	newCore = func() core.Core {
		fake := &fakeCore{}
		lastFakeCore = fake
		return fake
	}
	validateConfig = func(string, []byte) error { return nil }
	t.Cleanup(func() {
		newCore = oldCore
		validateConfig = oldValidate
		lastFakeCore = nil
	})
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
	for _, secret := range []string{"super-controller-secret", "alice", "hunter2"} {
		if strings.Contains(output, secret) {
			t.Errorf("config show leaked %q:\n%s", secret, output)
		}
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
	if secret, _ := cfg["secret"].(string); secret == "" {
		t.Error("run did not set a controller secret")
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
	if err := os.WriteFile(configPath, []byte("port: 7890\nproxies:\n  - name: node\n    type: ss\n"), 0o600); err != nil {
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
	secret, ok := cfg["secret"].(string)
	if !ok || secret == "" {
		t.Errorf("serve did not persist a random secret: %#v", cfg["secret"])
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
