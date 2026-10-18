//go:build unix

package runner

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func configureProcessGroup(cmd *exec.Cmd, isolate bool) {
	if isolate {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
}

func forwardedSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
}

func signalChild(process *os.Process, sig os.Signal, isolate bool) error {
	if !isolate {
		return process.Signal(sig)
	}
	signal, ok := sig.(syscall.Signal)
	if !ok {
		return process.Signal(sig)
	}
	return syscall.Kill(-process.Pid, signal)
}

func cleanupChild(process *os.Process, isolate bool) error {
	if !isolate {
		return process.Kill()
	}
	pid := process.Pid
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	// SIGKILL is immediate, but descendants may still be reaped by init. Wait
	// briefly so run returns only after the process group is actually gone.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(-pid, 0); errors.Is(err, syscall.ESRCH) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

func processExitCode(state *os.ProcessState) int {
	status, ok := state.Sys().(syscall.WaitStatus)
	if !ok {
		return state.ExitCode()
	}
	if status.Signaled() {
		return 128 + int(status.Signal())
	}
	return status.ExitStatus()
}
