//go:build unix

package runner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunPropagatesExitCode(t *testing.T) {
	code, err := New(Options{}).Run(t.Context(), "sh", []string{"-c", "exit 7"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
}

func TestRunCancellationKillsAfterGrace(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	code, err := New(Options{Stdin: bytes.NewReader(nil), GracePeriod: 100 * time.Millisecond}).Run(ctx, "sh", []string{"-c", "trap '' TERM; sleep 10"})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 137 {
		t.Errorf("exit code = %d, want 137 (SIGKILL)", code)
	}
	if elapsed > 3*time.Second {
		t.Errorf("cancellation took %s, want less than 3s", elapsed)
	}
}

func TestRunTTYSignalEscalation(t *testing.T) {
	readyR, readyW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readyR.Close()
	defer readyW.Close()

	cmd := exec.Command("sh", "-c", `trap '' INT TERM; echo ready; while :; do sleep 0.05; done`)
	cmd.Stdin = bytes.NewReader(nil)
	cmd.Stdout = readyW
	cmd.Stderr = io.Discard

	resultCh := make(chan struct {
		code int
		err  error
	}, 1)
	go func() {
		code, err := New(Options{GracePeriod: 100 * time.Millisecond}).runCommand(context.Background(), cmd, false)
		resultCh <- struct {
			code int
			err  error
		}{code: code, err: err}
	}()

	ready := make([]byte, len("ready\n"))
	if _, err := io.ReadFull(readyR, ready); err != nil {
		t.Fatalf("read child readiness: %v", err)
	}
	readyW.Close()

	start := time.Now()
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("send SIGINT: %v", err)
	}

	result := <-resultCh
	elapsed := time.Since(start)
	if result.err != nil {
		t.Fatalf("runCommand: %v", result.err)
	}
	if result.code != 137 {
		t.Errorf("exit code = %d, want 137 (SIGKILL)", result.code)
	}
	if elapsed < 150*time.Millisecond {
		t.Errorf("escalation took %s, want SIGTERM grace before SIGKILL", elapsed)
	}
	if elapsed > 3*time.Second {
		t.Errorf("escalation took too long: %s", elapsed)
	}
}

func TestRunKillsProcessGroupWhenLeaderExits(t *testing.T) {
	dir := t.TempDir()
	pidFile := dir + "/child.pid"
	readyR, readyW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readyR.Close()
	defer readyW.Close()

	script := `trap 'exit 0' TERM
sh -c 'trap "" TERM HUP INT; sleep 30' &
echo $! > "$1"
echo ready
wait
`
	cmd := exec.Command("sh", "-c", script, "sh", pidFile)
	cmd.Stdin = bytes.NewReader(nil)
	cmd.Stdout = readyW
	cmd.Stderr = io.Discard
	configureProcessGroup(cmd, true)

	resultCh := make(chan struct {
		code int
		err  error
	}, 1)
	go func() {
		code, err := New(Options{GracePeriod: time.Second}).runCommand(context.Background(), cmd, true)
		resultCh <- struct {
			code int
			err  error
		}{code: code, err: err}
	}()

	ready := make([]byte, len("ready\n"))
	if _, err := io.ReadFull(readyR, ready); err != nil {
		t.Fatalf("read child readiness: %v", err)
	}
	readyW.Close()

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	result := <-resultCh
	if result.err != nil {
		t.Fatalf("runCommand: %v", result.err)
	}

	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read grandchild pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		t.Fatalf("parse grandchild pid: %v", err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Errorf("grandchild %d survived process-group cleanup: %v", pid, err)
	}
}
