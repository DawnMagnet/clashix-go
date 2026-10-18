//go:build unix

package cli

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"
)

type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func TestServeSurvivesSIGHUPDuringStartup(t *testing.T) {
	port := freeServePort(t)
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		time.Sleep(1200 * time.Millisecond)
		fmt.Fprintf(w, "mixed-port: %d\nmode: rule\nproxies: []\nrules:\n  - MATCH,DIRECT\n", port)
	}))
	defer server.Close()

	cmd := exec.Command(os.Args[0], "-test.run=^TestServeSignalHelper$")
	var output safeBuffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	cmd.Env = append(os.Environ(),
		"CX_SERVE_SIGNAL_HELPER=1",
		"CX_TEST_SUB="+server.URL,
		"CX_TEST_DIR="+t.TempDir(),
		"CX_TEST_PORT="+strconv.Itoa(port),
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start serve helper: %v", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
	})

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatalf("serve did not start fetching the subscription:\n%s", output.String())
	}
	if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatalf("send SIGHUP: %v", err)
	}

	waitForOutput(t, &output, 10*time.Second, "cx serve: configuration active")
	waitForOutput(t, &output, 10*time.Second, "cx serve: reloaded configuration (SIGHUP)")
	select {
	case err := <-exited:
		t.Fatalf("serve exited after SIGHUP during startup: %v\n%s", err, output.String())
	default:
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("serve did not exit cleanly: %v\n%s", err, output.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("serve did not stop after SIGTERM:\n%s", output.String())
	}
}

func TestServeSignalHelper(t *testing.T) {
	if os.Getenv("CX_SERVE_SIGNAL_HELPER") != "1" {
		t.Skip("subprocess helper")
	}
	root := NewRoot("test")
	root.SetArgs([]string{
		"serve",
		"--sub", os.Getenv("CX_TEST_SUB"),
		"--dir", os.Getenv("CX_TEST_DIR"),
		"--mixed-port", os.Getenv("CX_TEST_PORT"),
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("serve: %v", err)
	}
}

func freeServePort(t *testing.T) int {
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

func waitForOutput(t *testing.T, output *safeBuffer, timeout time.Duration, want string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if bytes.Contains([]byte(output.String()), []byte(want)) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("output does not contain %q within %s:\n%s", want, timeout, output.String())
}
