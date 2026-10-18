package cli

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/DawnMagnet/clashix-go/internal/config"
	"github.com/DawnMagnet/clashix-go/internal/core"
	"github.com/DawnMagnet/clashix-go/internal/ui"
	"github.com/spf13/cobra"
)

var (
	panelBundled = ui.Bundled
	extractPanel = ui.Extract
)

func newServeCmd() *cobra.Command {
	var (
		dir         string
		subInterval time.Duration
		ui          bool
		uiAddr      string
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
			resolvedUI, uiSet := resolveUI(cmd, ui, uiAddr)
			if uiSet {
				overlay.ExternalController = config.StringPtr(resolvedUI)
			}

			ctx, stopSignals := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stopSignals()

			cfg, err := loadEffectiveConfig(ctx, &opts, overlay, true, configPath)
			if err != nil {
				return err
			}
			warnInsecureController(cmd, "serve", cfg)
			secret, generated, err := ensureControllerSecret(cfg, "")
			if err != nil {
				return err
			}
			if generated {
				logControllerSecret(cmd, "serve", secret)
			}
			panelURL, err := setupPanel(stateDir, cfg, true, true, "serve", cmd)
			if err != nil {
				return err
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
			logPanelURL(cmd, "serve", panelURL)

			reload := func(reason string) {
				reloadOverlay := opts.overlay(cmd)
				if uiSet {
					reloadOverlay.ExternalController = config.StringPtr(resolvedUI)
				}
				newCfg, err := loadEffectiveConfig(ctx, &opts, reloadOverlay, true, configPath)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "cx serve: reload (%s) failed: %v\n", reason, err)
					return
				}
				warnInsecureController(cmd, "serve", newCfg)
				newSecret, generated, err := ensureControllerSecret(newCfg, secret)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "cx serve: reload (%s) secret failed: %v\n", reason, err)
					return
				}
				if generated {
					logControllerSecret(cmd, "serve", newSecret)
				}
				newPanelURL, err := setupPanel(stateDir, newCfg, true, false, "serve", cmd)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "cx serve: reload (%s) panel failed: %v\n", reason, err)
					return
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
				if newPanelURL != "" && newPanelURL != panelURL {
					panelURL = newPanelURL
					logPanelURL(cmd, "serve", panelURL)
				}
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
	addUIFlag(cmd, &ui, &uiAddr)
	addConfigFlags(cmd, &opts)
	return cmd
}

func newRunCmd() *cobra.Command {
	var (
		opts       configOptions
		controller string
		ui         bool
		uiAddr     string
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
			resolvedUI, uiSet := resolveUI(cmd, ui, uiAddr)
			if !cmd.Flags().Changed("ui") && !cmd.Flags().Changed("ui-addr") && cmd.Flags().Changed("controller") {
				// An explicit --controller wins over the CX_UI / CX_UI_ADDR environment.
				resolvedUI, uiSet = "", false
			}
			if uiSet {
				controller = resolvedUI
			}
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
			// Run mode exposes only the explicitly requested main controller.
			for _, key := range []string{"external-controller-tls", "external-controller-unix", "external-controller-pipe"} {
				delete(cfg, key)
			}
			warnInsecureController(cmd, "run", cfg)
			secret, generated, err := ensureControllerSecret(cfg, "")
			if err != nil {
				return err
			}
			if generated {
				logControllerSecret(cmd, "run", secret)
			}

			stateDir, err := os.MkdirTemp("", "cx-run-*")
			if err != nil {
				return fmt.Errorf("create temporary state directory: %w", err)
			}
			defer func() {
				_ = os.RemoveAll(stateDir)
			}()
			panelURL, err := setupPanel(stateDir, cfg, uiSet, true, "run", cmd)
			if err != nil {
				return err
			}
			data, err := config.Marshal(cfg)
			if err != nil {
				return err
			}
			if err := validateConfig(stateDir, data); err != nil {
				return err
			}
			if err := config.WriteYAML(config.DefaultConfigPath(stateDir), data); err != nil {
				return err
			}

			mixedAddress := fmt.Sprintf("127.0.0.1:%d", mixedPort)
			if err := core.CheckPortFree(mixedAddress); err != nil {
				return err
			}
			engine := newCore()
			if err := engine.Start(ctx, stateDir, data); err != nil {
				return fmt.Errorf("start core: %w", err)
			}
			defer func() {
				_ = engine.Stop()
			}()
			if err := core.CheckPortInUse(mixedAddress); err != nil {
				return fmt.Errorf("mixed proxy failed to bind: %w", err)
			}
			logPanelURL(cmd, "run", panelURL)
			if err := core.WaitReady(ctx, mixedAddress, core.DefaultReadyTimeout); err != nil {
				return fmt.Errorf("proxy did not become ready: %w", err)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			return runChild(ctx, cmd, args, mixedPort)
		},
	}
	cmd.Flags().StringVar(&controller, "controller", "", "external controller address (disabled when empty)")
	addUIFlag(cmd, &ui, &uiAddr)
	addConfigFlags(cmd, &opts)
	return cmd
}

func ensureControllerSecret(cfg map[string]any, fallback string) (string, bool, error) {
	if secret, ok := cfg["secret"].(string); ok && secret != "" {
		return secret, false, nil
	}
	controller, _ := cfg["external-controller"].(string)
	controllerTLS, _ := cfg["external-controller-tls"].(string)
	if strings.TrimSpace(controller) == "" && strings.TrimSpace(controllerTLS) == "" {
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

func resolveUI(cmd *cobra.Command, enabled bool, address string) (string, bool) {
	// --ui-addr always implies --ui, including when --ui=false is passed.
	if cmd.Flags().Changed("ui-addr") {
		return uiAddress(address), true
	}
	if cmd.Flags().Changed("ui") {
		if !enabled {
			return "", false
		}
		return uiAddress(address), true
	}

	if value := strings.TrimSpace(os.Getenv("CX_UI")); value != "" {
		on, legacyAddress := parseUIEnv(value)
		if !on {
			return "", false
		}
		if addr := strings.TrimSpace(os.Getenv("CX_UI_ADDR")); addr != "" {
			return addr, true
		}
		if legacyAddress != "" {
			return legacyAddress, true
		}
		return defaultUIAddress, true
	}
	if addr := strings.TrimSpace(os.Getenv("CX_UI_ADDR")); addr != "" {
		return addr, true
	}
	return "", false
}

func uiAddress(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return defaultUIAddress
}

// parseUIEnv accepts boolean-like CX_UI values. An address is also accepted for
// compatibility with the earlier CX_UI=ADDR form.
func parseUIEnv(value string) (bool, string) {
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on", "enabled":
		return true, ""
	case "0", "false", "no", "off", "disabled":
		return false, ""
	}
	if strings.Contains(value, ":") {
		return true, value
	}
	return false, ""
}

func controllerEndpoints(cfg map[string]any) (main, tls string) {
	main, _ = cfg["external-controller"].(string)
	tls, _ = cfg["external-controller-tls"].(string)
	return strings.TrimSpace(main), strings.TrimSpace(tls)
}

func warnInsecureController(cmd *cobra.Command, mode string, cfg map[string]any) {
	if secret, ok := cfg["secret"].(string); ok && secret != "" {
		return
	}
	main, tls := controllerEndpoints(cfg)
	for _, address := range []string{main, tls} {
		if address != "" && !isLoopbackAddress(address) {
			fmt.Fprintf(cmd.ErrOrStderr(), "WARNING: cx %s: controller %s is not loopback and no secret is configured; a random secret will be generated\n", mode, address)
			return
		}
	}
}

func isLoopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func setupPanel(stateDir string, cfg map[string]any, allow, refresh bool, mode string, cmd *cobra.Command) (string, error) {
	main, tls := controllerEndpoints(cfg)
	if !allow || (main == "" && tls == "") {
		return "", nil
	}

	externalUI, _ := cfg["external-ui"].(string)
	if panelBundled() {
		target := filepath.Join(stateDir, "ui")
		if refresh {
			extracted, err := extractPanel(stateDir)
			if err != nil {
				return "", fmt.Errorf("extract web panel: %w", err)
			}
			target = extracted
		} else if _, err := os.Stat(filepath.Join(target, "index.html")); err != nil {
			return "", fmt.Errorf("bundled web panel directory %q is missing", target)
		}
		cfg["external-ui"] = target
		externalUI = target
	}

	if strings.TrimSpace(externalUI) == "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "cx %s: web panel assets are not bundled in this build; controller API remains available\n", mode)
		return "", nil
	}
	url, ok := panelURL(cfg)
	if !ok {
		return "", nil
	}
	return url, nil
}

func panelURL(cfg map[string]any) (string, bool) {
	main, tls := controllerEndpoints(cfg)
	address, scheme := main, "http"
	if address == "" {
		address, scheme = tls, "https"
	}
	if address == "" {
		return "", false
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return scheme + "://" + address + "/ui/", true
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return scheme + "://" + net.JoinHostPort(host, port) + "/ui/", true
}

func logPanelURL(cmd *cobra.Command, mode, panelURL string) {
	if panelURL != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "cx %s: web panel: %s\n", mode, panelURL)
	}
}
