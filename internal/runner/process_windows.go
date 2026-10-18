//go:build windows

package runner

import (
	"os"
	"os/exec"
	"syscall"
)

const createNewProcessGroup = 0x00000200

func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

func forwardedSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

func signalProcessGroup(process *os.Process, sig os.Signal) error {
	if sig == os.Interrupt {
		return process.Signal(sig)
	}
	return process.Kill()
}

func killProcessGroup(process *os.Process) error {
	return process.Kill()
}

func processExitCode(state *os.ProcessState) int {
	return state.ExitCode()
}
