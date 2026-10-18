package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/DawnMagnet/clashix-go/internal/runner"
	"github.com/spf13/cobra"
)

func runnerFreePort() (int, error) {
	return runner.FreePort()
}

func runChild(cmd *cobra.Command, args []string, mixedPort int) error {
	engine := runner.New(runner.Options{
		Env:    runner.BuildEnv(os.Environ(), mixedPort),
		Stdin:  cmd.InOrStdin(),
		Stdout: cmd.OutOrStdout(),
		Stderr: cmd.ErrOrStderr(),
	})
	code, err := engine.Run(context.Background(), args[0], args[1:])
	if err != nil {
		return fmt.Errorf("run %q: %w", args[0], err)
	}
	if code != 0 {
		return &ExitError{Code: code}
	}
	return nil
}
