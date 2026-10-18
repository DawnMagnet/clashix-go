package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/DawnMagnet/clashix-go/internal/config"
	"github.com/DawnMagnet/clashix-go/internal/core"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var (
		dir  string
		opts configOptions
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the proxy service in the foreground",
		Long:  "Run the proxy service in the foreground. Stop it with SIGINT or SIGTERM.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.validatePorts(cmd); err != nil {
				return err
			}
			stateDir, err := resolveStateDir(dir)
			if err != nil {
				return err
			}
			configPath := config.DefaultConfigPath(stateDir)

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			overlay := opts.overlay(cmd)
			cfg, err := loadEffectiveConfig(ctx, &opts, overlay, true, configPath)
			if err != nil {
				return err
			}
			if _, err := config.EnsureSecret(cfg); err != nil {
				return err
			}
			data, err := config.Marshal(cfg)
			if err != nil {
				return err
			}
			if err := config.WriteYAML(configPath, data); err != nil {
				return err
			}

			engine := core.New()
			if err := engine.Start(ctx, stateDir, data); err != nil {
				return fmt.Errorf("start core: %w", err)
			}

			<-ctx.Done()
			if err := engine.Stop(); err != nil {
				return fmt.Errorf("stop core: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "state directory (default: user config directory/cx)")
	addConfigFlags(cmd, &opts)
	return cmd
}

func newRunCmd() *cobra.Command {
	var opts configOptions

	cmd := &cobra.Command{
		Use:   "run [flags] -- <command> [args...]",
		Short: "Run a command through an ephemeral proxy",
		Long:  "Start an ephemeral proxy, run a command with proxy environment variables, then stop the proxy and remove its state directory.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.validatePorts(cmd); err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			overlay := opts.overlay(cmd)
			mixedPort := opts.mixedPort
			if !cmd.Flags().Changed("mixed-port") {
				port, err := runnerFreePort()
				if err != nil {
					return err
				}
				mixedPort = port
				overlay.MixedPort = config.IntPtr(port)
			}

			cfg, err := loadEffectiveConfig(ctx, &opts, overlay, true, "")
			if err != nil {
				return err
			}
			if _, err := config.EnsureSecret(cfg); err != nil {
				return err
			}
			data, err := config.Marshal(cfg)
			if err != nil {
				return err
			}

			stateDir, err := os.MkdirTemp("", "cx-run-*")
			if err != nil {
				return fmt.Errorf("create temporary state directory: %w", err)
			}
			defer func() {
				_ = os.RemoveAll(stateDir)
			}()
			if err := config.WriteYAML(config.DefaultConfigPath(stateDir), data); err != nil {
				return err
			}

			engine := core.New()
			if err := engine.Start(ctx, stateDir, data); err != nil {
				return fmt.Errorf("start core: %w", err)
			}

			runErr := runChild(cmd, args, mixedPort)
			if err := engine.Stop(); err != nil {
				return fmt.Errorf("stop core: %w", err)
			}
			return runErr
		},
	}
	addConfigFlags(cmd, &opts)
	return cmd
}
