//go:build linux

package notify

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func readNotification(t *testing.T, conn *net.UnixConn) string {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read notification: %v", err)
	}
	return string(buf[:n])
}

func TestNotifierUnixgram(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notify.sock")
	listener, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("NOTIFY_SOCKET", path)

	notifier := New()
	defer notifier.Close()

	notifier.Ready("configuration active")
	if got := readNotification(t, listener); !strings.Contains(got, "READY=1") || !strings.Contains(got, "STATUS=configuration active") {
		t.Errorf("ready notification = %q", got)
	}

	notifier.Reloading("reloading configuration")
	got := readNotification(t, listener)
	if !strings.Contains(got, "RELOADING=1") || !strings.Contains(got, "STATUS=reloading configuration") {
		t.Errorf("reloading notification = %q", got)
	}
	var monotonic string
	for _, line := range strings.Split(got, "\n") {
		if value, ok := strings.CutPrefix(line, "MONOTONIC_USEC="); ok {
			monotonic = value
		}
	}
	if usec, err := strconv.ParseUint(monotonic, 10, 64); err != nil || usec == 0 {
		t.Errorf("MONOTONIC_USEC = %q, want a positive integer", monotonic)
	}

	notifier.Status("working")
	if got := readNotification(t, listener); got != "STATUS=working" {
		t.Errorf("status notification = %q", got)
	}

	notifier.Stopping("shutting down")
	if got := readNotification(t, listener); !strings.Contains(got, "STOPPING=1") || !strings.Contains(got, "STATUS=shutting down") {
		t.Errorf("stopping notification = %q", got)
	}
}

func TestNotifierAbstractSocket(t *testing.T) {
	name := fmt.Sprintf("@cx-notify-%d", os.Getpid())
	listener, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: name, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("NOTIFY_SOCKET", name)

	notifier := New()
	defer notifier.Close()
	notifier.Ready("abstract socket")
	if got := readNotification(t, listener); !strings.Contains(got, "READY=1") {
		t.Errorf("ready notification = %q", got)
	}
}

func TestNotifierNoSocket(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	notifier := New()
	notifier.Status("ignored")
	notifier.Reloading("ignored")
	notifier.Ready("ignored")
	notifier.Stopping("ignored")
	if err := notifier.Close(); err != nil {
		t.Fatal(err)
	}
}
