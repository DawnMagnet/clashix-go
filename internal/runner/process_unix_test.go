//go:build unix

package runner

import (
	"bytes"
	"context"
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
