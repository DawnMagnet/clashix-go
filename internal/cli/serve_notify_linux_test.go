//go:build linux

package cli

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readNotificationUntil(t *testing.T, conn *net.UnixConn, marker string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	buf := make([]byte, 4096)
	for time.Now().Before(deadline) {
		if err := conn.SetReadDeadline(deadline); err != nil {
			t.Fatal(err)
		}
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatalf("read notification waiting for %q: %v", marker, err)
		}
		message := string(buf[:n])
		if strings.Contains(message, marker) {
			return message
		}
	}
	t.Fatalf("did not receive %q", marker)
	return ""
}

func TestServeSendsSystemdNotifications(t *testing.T) {
	useFakeCore(t)
	path := filepath.Join(t.TempDir(), "notify.sock")
	listener, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("NOTIFY_SOCKET", path)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := NewRoot("dev")
	root.SetContext(ctx)
	root.SetArgs([]string{"serve", "--config", writeConfig(t), "--dir", t.TempDir()})
	done := make(chan error, 1)
	go func() { done <- root.Execute() }()

	readNotificationUntil(t, listener, "READY=1", 2*time.Second)
	cancel()
	readNotificationUntil(t, listener, "STOPPING=1", 2*time.Second)
	if err := <-done; err != nil {
		t.Fatalf("Execute: %v", err)
	}
}
