// Package runner starts child processes with a proxy environment, forwards
// signals, and returns the child's exit code.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/term"
)

// DefaultGracePeriod is how long a child process group gets to exit after a
// termination signal before it is killed.
const DefaultGracePeriod = 5 * time.Second

// Options controls child process execution.
type Options struct {
	Env         []string
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	GracePeriod time.Duration
}

// Runner executes a child process.
type Runner struct {
	Options Options
}

// New returns a Runner with the supplied options.
func New(options Options) *Runner {
	return &Runner{Options: options}
}

// Run starts name with args, forwards termination signals to its process
// group, and returns its exit code. A child that exits non-zero is reported
// through the returned code, not as an error.
func (r *Runner) Run(ctx context.Context, name string, args []string) (int, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = r.Options.Env
	cmd.Stdin = r.Options.Stdin
	if cmd.Stdin == nil {
		cmd.Stdin = os.Stdin
	}
	cmd.Stdout = r.Options.Stdout
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = r.Options.Stderr
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}
	isolateGroup := shouldIsolateProcessGroup(cmd.Stdin)
	configureProcessGroup(cmd, isolateGroup)
	return r.runCommand(ctx, cmd, isolateGroup)
}

func (r *Runner) runCommand(ctx context.Context, cmd *exec.Cmd, isolateGroup bool) (int, error) {
	if err := ctx.Err(); err != nil {
		return -1, err
	}
	name := cmd.Args[0]

	signalCh := make(chan os.Signal, 8)
	signal.Notify(signalCh, forwardedSignals()...)
	defer signal.Stop(signalCh)

	if err := cmd.Start(); err != nil {
		return -1, fmt.Errorf("start %q: %w", name, err)
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	grace := r.Options.GracePeriod
	if grace <= 0 {
		grace = DefaultGracePeriod
	}

	type escalation int
	const (
		escalationNone escalation = iota
		escalationTTYInterrupt
		escalationKill
	)

	var (
		graceTimer  *time.Timer
		graceC      <-chan time.Time
		stage       escalation
		terminating bool
	)
	startGrace := func(next escalation) {
		if graceTimer != nil {
			if next == escalationKill {
				stage = escalationKill
			}
			return
		}
		stage = next
		graceTimer = time.NewTimer(grace)
		graceC = graceTimer.C
	}
	defer func() {
		if graceTimer != nil {
			graceTimer.Stop()
		}
	}()

	ctxDone := ctx.Done()
	for {
		select {
		case err := <-waitCh:
			if terminating && isolateGroup {
				// The leader can exit while descendants survive. Finish cleanup
				// of the isolated group before reporting the exit.
				_ = cleanupChild(cmd.Process, true)
			}
			if err == nil {
				return 0, nil
			}
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return processExitCode(exitErr.ProcessState), nil
			}
			return -1, fmt.Errorf("wait for %q: %w", name, err)
		case sig := <-signalCh:
			// With a terminal on stdin the child shares our foreground process
			// group, so the terminal has already delivered SIGINT to it. Do not
			// double-forward SIGINT, but start escalation in case it ignores it.
			if sig == os.Interrupt && !isolateGroup {
				terminating = true
				startGrace(escalationTTYInterrupt)
				continue
			}
			// Best effort: the child may have exited between Wait and forwarding.
			_ = signalChild(cmd.Process, sig, isolateGroup)
			if isTerminationSignal(sig) {
				terminating = true
				startGrace(escalationKill)
			}
		case <-graceC:
			graceC = nil
			if stage == escalationTTYInterrupt {
				// The terminal already sent SIGINT. Give the child a second
				// chance with SIGTERM before forcing it down.
				_ = signalChild(cmd.Process, syscall.SIGTERM, false)
				stage = escalationKill
				graceTimer.Reset(grace)
				graceC = graceTimer.C
				continue
			}
			_ = cleanupChild(cmd.Process, isolateGroup)
			stage = escalationNone
		case <-ctxDone:
			ctxDone = nil
			terminating = true
			// Best effort: the child may already be gone.
			_ = signalChild(cmd.Process, syscall.SIGTERM, isolateGroup)
			startGrace(escalationKill)
		}
	}
}

func isTerminationSignal(sig os.Signal) bool {
	return sig == os.Interrupt || sig == syscall.SIGTERM
}

// shouldIsolateProcessGroup reports whether the child needs its own process
// group. Interactive children must stay in cx's foreground group so terminal
// job-control signals continue to work.
func shouldIsolateProcessGroup(stdin io.Reader) bool {
	file, ok := stdin.(*os.File)
	if !ok {
		return true
	}
	return !term.IsTerminal(int(file.Fd()))
}
