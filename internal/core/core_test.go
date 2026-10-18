package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DawnMagnet/clashix-go/internal/config"
	"github.com/sirupsen/logrus"
)

func TestValidate(t *testing.T) {
	dir := t.TempDir()
	valid := []byte("mixed-port: 0\nmode: rule\nproxies: []\nrules:\n  - MATCH,DIRECT\n")
	if err := Validate(dir, valid); err != nil {
		t.Fatalf("Validate valid config: %v", err)
	}
	if err := Validate(dir, []byte("mixed-port: not-a-number\n")); err == nil {
		t.Fatal("Validate accepted an invalid config")
	}
}

func TestValidateOnlyLogsWarningsAndErrors(t *testing.T) {
	dir := t.TempDir()
	var logs bytes.Buffer
	oldOutput := logrus.StandardLogger().Out
	logrus.SetOutput(&logs)
	defer logrus.SetOutput(oldOutput)

	cfg := []byte("mixed-port: 0\nlog-level: debug\nmode: rule\nproxies: []\nrules:\n  - MATCH,DIRECT\n")
	if err := Validate(dir, cfg); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if strings.Contains(logs.String(), "level=info") {
		t.Errorf("validation emitted info logs: %s", logs.String())
	}
}

func TestTunExampleMergesAndValidates(t *testing.T) {
	base := map[string]any{
		"mixed-port": 0,
		"mode":       "rule",
		"proxies":    []any{},
		"rules":      []any{"MATCH,DIRECT"},
	}
	override, ok, err := config.LoadOverride(filepath.Join("..", "..", "examples", "override-tun-server.yaml"), false)
	if err != nil || !ok {
		t.Fatalf("LoadOverride = %v, %v", ok, err)
	}
	merged := config.MergeOverride(base, override)
	data, err := config.Marshal(merged)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := Validate(t.TempDir(), data); err != nil {
		t.Fatalf("Validate TUN example: %v", err)
	}
	tun, ok := merged["tun"].(map[string]any)
	if !ok {
		t.Fatalf("tun = %#v", merged["tun"])
	}
	if tun["enable"] != true || tun["stack"] != "system" || tun["auto-route"] != true || tun["auto-detect-interface"] != true {
		t.Errorf("tun = %#v", tun)
	}
	routes, ok := tun["route-exclude-address"].([]any)
	if !ok || len(routes) != 11 {
		t.Errorf("route-exclude-address = %#v", tun["route-exclude-address"])
	}
	for _, want := range []string{"127.0.0.0/8", "224.0.0.0/4", "255.255.255.255/32", "ff00::/8"} {
		found := false
		for _, route := range routes {
			if route == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("route-exclude-address is missing %s", want)
		}
	}
	dns, ok := merged["dns"].(map[string]any)
	if !ok || dns["enable"] != true || dns["enhanced-mode"] != "fake-ip" {
		t.Errorf("dns = %#v", merged["dns"])
	}
	filters, ok := dns["fake-ip-filter"].([]any)
	if !ok || len(filters) == 0 {
		t.Errorf("fake-ip-filter = %#v", dns["fake-ip-filter"])
	}
}

func TestTunFailureIncludesUnderlyingError(t *testing.T) {
	err := tunFailure("system", "", "configure tun interface: operation not permitted")
	if !errors.Is(err, ErrTunNotStarted) {
		t.Fatalf("tunFailure error = %v, want ErrTunNotStarted", err)
	}
	for _, want := range []string{
		"configure tun interface: operation not permitted",
		"CAP_NET_ADMIN",
		"root",
		"/dev/net/tun",
		"PrivateDevices",
		"with_gvisor",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestTunFailureBusySuggestsTunDevice(t *testing.T) {
	err := tunFailure("system", "cxtun", "configure tun interface: device or resource busy")
	if !errors.Is(err, ErrTunNotStarted) {
		t.Fatalf("tunFailure error = %v, want ErrTunNotStarted", err)
	}
	for _, want := range []string{"cxtun", "already in use", "another process", "another cx instance", "--tun-device NAME"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestTunConfigured(t *testing.T) {
	if tunConfigured(map[string]any{"tun": map[string]any{"enable": true}}) != true {
		t.Error("tunConfigured missed an enabled TUN")
	}
	if tunConfigured(map[string]any{"tun": map[string]any{"enable": false}}) != false {
		t.Error("tunConfigured reported a disabled TUN")
	}
	if tunConfigured(map[string]any{}) != false {
		t.Error("tunConfigured reported TUN without a tun section")
	}
}

func TestWritePayloadGeodata(t *testing.T) {
	dir := t.TempDir()
	files := map[string][]byte{
		"geoip.metadb":      []byte("metadb"),
		"geoip.dat":         []byte("geoip"),
		"geosite.dat":       []byte("geosite"),
		"GeoLite2-ASN.mmdb": []byte("asn"),
		"unrelated.file":    []byte("ignore"),
		"config.yaml":       []byte("port: 1\n"),
	}
	if err := writePayloadGeodata(dir, files); err != nil {
		t.Fatalf("writePayloadGeodata: %v", err)
	}
	for name, want := range files {
		if name == "unrelated.file" || name == "config.yaml" {
			if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("%s should not have been written", name)
			}
			continue
		}
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
		if runtime.GOOS != "windows" {
			info, err := os.Stat(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Errorf("%s mode = %o, want 600", name, info.Mode().Perm())
			}
		}
	}
	asn, err := os.ReadFile(filepath.Join(dir, "ASN.mmdb"))
	if err != nil {
		t.Fatalf("read ASN.mmdb alias: %v", err)
	}
	if string(asn) != "asn" {
		t.Errorf("ASN.mmdb = %q, want asn", asn)
	}
}

func TestWaitReadyTimeout(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := WaitReady(ctx, address, time.Second); err == nil {
		t.Fatal("WaitReady succeeded on a closed port")
	}
}

func TestMihomoProxyIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping mihomo integration test in short mode")
	}

	port := freePort(t)
	dir := t.TempDir()
	cfg := fmt.Sprintf(`mixed-port: %d
allow-lan: false
mode: rule
log-level: warning
proxies: []
rules:
  - MATCH,DIRECT
`, port)

	core := NewMihomo()
	ctx := context.Background()
	if err := core.Start(ctx, dir, []byte("mixed-port: not-a-number\n")); err == nil {
		t.Fatal("Start accepted an invalid config")
	}
	if err := core.Start(ctx, dir, []byte(cfg)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	stopped := false
	defer func() {
		if !stopped {
			_ = core.Stop()
		}
	}()

	address := fmt.Sprintf("127.0.0.1:%d", port)
	if err := WaitReady(ctx, address, 15*time.Second); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "proxy-ok")
	}))
	defer server.Close()

	proxyURL, err := url.Parse("http://" + address)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   10 * time.Second,
	}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("request through proxy: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || string(body) != "proxy-ok" {
		t.Fatalf("proxy response = %d %q", resp.StatusCode, body)
	}

	var shutdownLogs bytes.Buffer
	oldOutput := logrus.StandardLogger().Out
	logrus.SetOutput(&shutdownLogs)
	stopErr := core.Stop()
	logrus.SetOutput(oldOutput)
	if stopErr != nil {
		t.Fatalf("Stop: %v", stopErr)
	}
	stopped = true
	if strings.Contains(shutdownLogs.String(), "level=info") {
		t.Errorf("shutdown emitted info logs: %s", shutdownLogs.String())
	}

	if conn, err := net.DialTimeout("tcp", address, 200*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Error("mixed listener still accepts after Stop")
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Errorf("state directory is still locked after Stop: %v", err)
	}

	if err := NewMihomo().Start(ctx, dir, []byte(cfg)); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("second Start error = %v, want ErrAlreadyStarted", err)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func TestLogsGoToStderr(t *testing.T) {
	filter, ok := logrus.StandardLogger().Out.(*expectedErrorFilter)
	if !ok {
		t.Fatalf("mihomo log output = %T, want filtered stderr", logrus.StandardLogger().Out)
	}
	if filter.writer != os.Stderr {
		t.Errorf("filtered log target = %T, want stderr", filter.writer)
	}
}

func TestExpectedErrorFilter(t *testing.T) {
	var output bytes.Buffer
	filter := &expectedErrorFilter{writer: &output}
	if _, err := filter.Write([]byte("keep this line\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := filter.Write([]byte(`time="now" level=error msg="External controller serve error: http: Server closed"` + "\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := filter.Write([]byte(`time="now" level=warning msg="Mihomo shutting down"` + "\n")); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "keep this line\n" {
		t.Errorf("filtered output = %q", got)
	}
}
