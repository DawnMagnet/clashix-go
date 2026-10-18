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
	for _, secret := range []string{"super-controller-secret", "alice", "hunter2", "provider-user", "provider-pass"} {
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
	if !strings.Contains(stderr.String(), "generated controller secret: "+secret) {
		t.Errorf("serve did not log the generated secret: %q", stderr.String())
	}

	if _, generated, err := ensureControllerSecret(cfg, secret); err != nil || generated {
		t.Errorf("second startup regenerated the secret: generated=%v err=%v", generated, err)
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
	if !strings.Contains(stderr.String(), "generated controller secret: "+secret) {
		t.Errorf("run did not log the generated secret: %q", stderr.String())
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
	if !strings.Contains(stderr.String(), "generated controller secret: "+secret) {
		t.Errorf("serve did not log the TLS controller secret: %q", stderr.String())
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
