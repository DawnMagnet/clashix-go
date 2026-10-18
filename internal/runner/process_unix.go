//go:build unix

package runner

import (
	"os"
	"os/exec"
	"syscall"
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

func killChild(process *os.Process, isolate bool) error {
	if !isolate {
		return process.Kill()
	}
	return syscall.Kill(-process.Pid, syscall.SIGKILL)
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
