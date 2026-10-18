//go:build windows

package runner

import (
	"os"
	"os/exec"
	"syscall"
)

const createNewProcessGroup = 0x00000200

func configureProcessGroup(cmd *exec.Cmd, isolate bool) {
	if isolate {
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
	}
}

func forwardedSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

func signalChild(process *os.Process, sig os.Signal, _ bool) error {
	if sig == os.Interrupt {
		return process.Signal(sig)
	}
	return process.Kill()
}

func killChild(process *os.Process, _ bool) error {
	return process.Kill()
}

func processExitCode(state *os.ProcessState) int {
	return state.ExitCode()
}
