package cli

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/DawnMagnet/clashix-go/internal/config"
	"github.com/DawnMagnet/clashix-go/internal/core"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var (
		dir         string
		subInterval time.Duration
		opts        configOptions
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the proxy service in the foreground",
		Long:  "Run the proxy service in the foreground. Stop it with SIGINT or SIGTERM. On Unix, SIGHUP reloads the configuration sources.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.validatePorts(cmd); err != nil {
				return err
			}
			if subInterval < 0 {
				return fmt.Errorf("--sub-interval must not be negative")
			}
			stateDir, err := resolveStateDir(dir)
			if err != nil {
				return err
			}
			configPath := config.DefaultConfigPath(stateDir)
			overlay := opts.overlay(cmd)

			ctx, stopSignals := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stopSignals()

			cfg, err := loadEffectiveConfig(ctx, &opts, overlay, true, configPath)
			if err != nil {
				return err
			}
			secret, generated, err := ensureControllerSecret(cfg, "")
			if err != nil {
				return err
			}
			if generated {
				logControllerSecret(cmd, "serve", secret)
			}
			data, err := config.Marshal(cfg)
			if err != nil {
				return err
			}
			if err := validateConfig(stateDir, data); err != nil {
				return err
			}
			if err := config.WriteYAML(configPath, data); err != nil {
				return err
			}

			engine := newCore()
			if err := engine.Start(ctx, stateDir, data); err != nil {
				return fmt.Errorf("start core: %w", err)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "cx serve: configuration active in %s\n", stateDir)

			reload := func(reason string) {
				newCfg, err := loadEffectiveConfig(ctx, &opts, opts.overlay(cmd), true, configPath)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "cx serve: reload (%s) failed: %v\n", reason, err)
					return
				}
				newSecret, generated, err := ensureControllerSecret(newCfg, secret)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "cx serve: reload (%s) secret failed: %v\n", reason, err)
					return
				}
				if generated {
					logControllerSecret(cmd, "serve", newSecret)
				}
				newData, err := config.Marshal(newCfg)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "cx serve: reload (%s) encode failed: %v\n", reason, err)
					return
				}
				if err := validateConfig(stateDir, newData); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "cx serve: reload (%s) validation failed: %v\n", reason, err)
					return
				}
				if err := config.WriteYAML(configPath, newData); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "cx serve: reload (%s) save failed: %v\n", reason, err)
					return
				}
				applier, ok := engine.(core.Applier)
				if !ok {
					fmt.Fprintf(cmd.ErrOrStderr(), "cx serve: reload (%s) unsupported by core\n", reason)
					return
				}
				if err := applier.Apply(stateDir, newData); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "cx serve: reload (%s) apply failed: %v\n", reason, err)
					return
				}
				secret = newSecret
				fmt.Fprintf(cmd.ErrOrStderr(), "cx serve: reloaded configuration (%s)\n", reason)
			}

			reloadCh := make(chan os.Signal, 1)
			if signals := reloadSignals(); len(signals) > 0 {
				signal.Notify(reloadCh, signals...)
				defer signal.Stop(reloadCh)
			}
			var tickerC <-chan time.Time
			if subInterval > 0 {
				ticker := time.NewTicker(subInterval)
				defer ticker.Stop()
				tickerC = ticker.C
			}

			for {
				select {
				case <-ctx.Done():
					if err := engine.Stop(); err != nil {
						return fmt.Errorf("stop core: %w", err)
					}
					return nil
				case <-reloadCh:
					reload("SIGHUP")
				case <-tickerC:
					reload("subscription interval")
				}
			}
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "state directory (default: user config directory/cx)")
	cmd.Flags().DurationVar(&subInterval, "sub-interval", 0, "periodically refresh subscriptions and reload")
	addConfigFlags(cmd, &opts)
	return cmd
}

func newRunCmd() *cobra.Command {
	var (
		opts       configOptions
		controller string
	)

	cmd := &cobra.Command{
		Use:   "run [flags] -- <command> [args...]",
		Short: "Run a command through an ephemeral proxy",
		Long:  "Start an ephemeral proxy, run a command with proxy environment variables, then stop the proxy and remove its state directory.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.validatePorts(cmd); err != nil {
				return err
			}

			ctx, stopSignals := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stopSignals()

			overlay := opts.overlay(cmd)
			logLevel := "warning"
			if cmd.Flags().Changed("log-level") {
				logLevel = opts.logLevel
			}
			overlay.LogLevel = config.StringPtr(logLevel)
			if err := core.SetLogLevel(logLevel); err != nil {
				return err
			}
			if !cmd.Flags().Changed("port") {
				overlay.Port = config.IntPtr(0)
			}
			if !cmd.Flags().Changed("socks-port") {
				overlay.SocksPort = config.IntPtr(0)
			}
			overlay.ExternalController = config.StringPtr(controller)

			mixedPort := opts.mixedPort
			if !cmd.Flags().Changed("mixed-port") {
				port, err := runnerFreePort()
				if err != nil {
					return err
				}
				mixedPort = port
				overlay.MixedPort = config.IntPtr(port)
			}

			cachedPath, err := defaultCachedConfigPath()
			if err != nil {
				return err
			}
			cfg, err := loadEffectiveConfig(ctx, &opts, overlay, true, cachedPath)
			if err != nil {
				return err
			}
			secret, generated, err := ensureControllerSecret(cfg, "")
			if err != nil {
				return err
			}
			if generated {
				logControllerSecret(cmd, "run", secret)
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
			if err := validateConfig(stateDir, data); err != nil {
				return err
			}
			if err := config.WriteYAML(config.DefaultConfigPath(stateDir), data); err != nil {
				return err
			}

			engine := newCore()
			if err := engine.Start(ctx, stateDir, data); err != nil {
				return fmt.Errorf("start core: %w", err)
			}
			defer func() {
				_ = engine.Stop()
			}()

			mixedAddress := fmt.Sprintf("127.0.0.1:%d", mixedPort)
			if err := core.WaitReady(ctx, mixedAddress, core.DefaultReadyTimeout); err != nil {
				return fmt.Errorf("proxy did not become ready: %w", err)
			}
			return runChild(cmd, args, mixedPort)
		},
	}
	cmd.Flags().StringVar(&controller, "controller", "", "external controller address (disabled when empty)")
	addConfigFlags(cmd, &opts)
	return cmd
}

func ensureControllerSecret(cfg map[string]any, fallback string) (string, bool, error) {
	if secret, ok := cfg["secret"].(string); ok && secret != "" {
		return secret, false, nil
	}
	controller, _ := cfg["external-controller"].(string)
	if strings.TrimSpace(controller) == "" {
		return "", false, nil
	}
	if fallback != "" {
		cfg["secret"] = fallback
		return fallback, false, nil
	}
	secret, err := config.GenerateSecret()
	if err != nil {
		return "", false, err
	}
	cfg["secret"] = secret
	return secret, true, nil
}

func logControllerSecret(cmd *cobra.Command, mode, secret string) {
	fmt.Fprintf(cmd.ErrOrStderr(), "cx %s: generated controller secret: %s\n", mode, secret)
}
