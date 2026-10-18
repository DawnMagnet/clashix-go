package cli

import (
	"errors"
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
	"golang.org/x/term"
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
			// Register signal handlers before any configuration loading or
			// subscription fetching. A SIGHUP during startup must not hit Go's
			// default action and kill the process.
			ctx, stop := signal.NotifyContext(cmd.Context(), stopSignals()...)
			defer stop()
			reloadCh := make(chan os.Signal, 1)
			if signals := reloadSignals(); len(signals) > 0 {
				signal.Notify(reloadCh, signals...)
				defer signal.Stop(reloadCh)
			}

			if err := opts.validatePorts(cmd); err != nil {
				return err
			}
			if err := opts.validateTun(cmd); err != nil {
				return err
			}
			if err := opts.validateOutbound(cmd); err != nil {
				return err
			}
			if err := opts.prepare(cmd); err != nil {
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
			opts.overrideDir = stateDir
			overlay := opts.overlay(cmd)
			resolvedUI, uiSet := resolveUI(cmd, ui, uiAddr)
			if uiSet {
				overlay.ExternalController = config.StringPtr(resolvedUI)
			}

			cfg, err := loadEffectiveConfig(ctx, &opts, overlay, true, configPath)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			if ctx.Err() != nil {
				return nil
			}
			autoInterface := applyAutoInterface(cmd, "serve", &opts, cfg)
			warnInsecureController(cmd, "serve", cfg)
			secret, err := ensureServeControllerSecret(cmd, stateDir, cfg, "")
			if err != nil {
				return err
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
			stateData, err := marshalStateConfig(cfg, autoInterface)
			if err != nil {
				return err
			}
			if err := config.WriteYAML(configPath, stateData); err != nil {
				return err
			}

			engine := newCore()
			if err := engine.Start(ctx, stateDir, data); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("start core: %w", err)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "cx serve: configuration active in %s\n", stateDir)
			logPanelURL(cmd, "serve", panelURL)
			lastGoodData := append([]byte(nil), data...)
			lastGoodStateData := append([]byte(nil), stateData...)

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
				newAutoInterface := applyAutoInterface(cmd, "serve", &opts, newCfg)
				warnInsecureController(cmd, "serve", newCfg)
				newSecret, err := ensureServeControllerSecret(cmd, stateDir, newCfg, secret)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "cx serve: reload (%s) secret failed: %v\n", reason, err)
					return
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
				newStateData, err := marshalStateConfig(newCfg, newAutoInterface)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "cx serve: reload (%s) encode failed: %v\n", reason, err)
					return
				}
				if err := config.WriteYAML(configPath, newStateData); err != nil {
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
					if errors.Is(err, core.ErrTunNotStarted) {
						if rollbackErr := applier.Apply(stateDir, lastGoodData); rollbackErr != nil {
							fmt.Fprintf(cmd.ErrOrStderr(), "cx serve: reload (%s) TUN rollback failed: %v\n", reason, rollbackErr)
						} else if saveErr := config.WriteYAML(configPath, lastGoodStateData); saveErr != nil {
							fmt.Fprintf(cmd.ErrOrStderr(), "cx serve: reload (%s) TUN rollback could not restore %s: %v\n", reason, configPath, saveErr)
						} else {
							fmt.Fprintf(cmd.ErrOrStderr(), "cx serve: reload (%s) rolled back to previous working configuration\n", reason)
						}
					}
					return
				}
				lastGoodData = append([]byte(nil), newData...)
				lastGoodStateData = append([]byte(nil), newStateData...)
				secret = newSecret
				if newPanelURL != "" && newPanelURL != panelURL {
					panelURL = newPanelURL
					logPanelURL(cmd, "serve", panelURL)
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "cx serve: reloaded configuration (%s)\n", reason)
			}

			// A SIGHUP received before the core was ready remains queued in
			// reloadCh and is coalesced into one reload by this loop.
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
					if ctx.Err() != nil {
						continue
					}
					reload("SIGHUP")
				case <-tickerC:
					if ctx.Err() != nil {
						continue
					}
					reload("subscription interval")
				}
			}
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "state directory (default: user config directory/cx)")
	cmd.Flags().DurationVar(&subInterval, "sub-interval", 0, "periodically refresh subscriptions and reload")
	addUIFlag(cmd, &ui, &uiAddr)
	addTunFlags(cmd, &opts)
	addOutboundFlags(cmd, &opts)
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
			if err := opts.validateTun(cmd); err != nil {
				return err
			}
			if err := opts.validateOutbound(cmd); err != nil {
				return err
			}
			if err := opts.prepare(cmd); err != nil {
				return err
			}
			if dir, err := config.DefaultDir(); err == nil {
				opts.overrideDir = dir
			}

			ctx, stopSignals := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stopSignals()

			overlay := opts.overlay(cmd)
			if !opts.allowLAN {
				overlay.AllowLAN = config.BoolPtr(false)
			}
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
			if !cmd.Flags().Changed("mixed-port") || mixedPort == 0 {
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
			applyAutoInterface(cmd, "run", &opts, cfg)
			if !opts.allowLAN {
				// run is temporary: it never exposes a LAN listener by default.
				cfg["bind-address"] = "127.0.0.1"
			}
			// Run mode exposes only the explicitly requested main controller.
			for _, key := range []string{"external-controller-tls", "external-controller-unix", "external-controller-pipe"} {
				delete(cfg, key)
			}
			warnInsecureController(cmd, "run", cfg)

			stateDir, err := os.MkdirTemp("", "cx-run-*")
			if err != nil {
				return fmt.Errorf("create temporary state directory: %w", err)
			}
			defer func() {
				_ = os.RemoveAll(stateDir)
			}()
			if _, err := ensureRunControllerSecret(cmd, stateDir, cfg); err != nil {
				return err
			}
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
				if ctx.Err() != nil {
					return nil
				}
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
	addTunFlags(cmd, &opts)
	addOutboundFlags(cmd, &opts)
	addConfigFlags(cmd, &opts)
	return cmd
}

func marshalStateConfig(cfg map[string]any, omitAutoInterface bool) ([]byte, error) {
	if !omitAutoInterface {
		return config.Marshal(cfg)
	}
	persisted := config.Clone(cfg)
	delete(persisted, "interface-name")
	return config.Marshal(persisted)
}

const controllerSecretName = "controller-secret"

var stderrIsTerminal = func() bool {
	return term.IsTerminal(int(os.Stderr.Fd()))
}

func controllerSecretPath(stateDir string) string {
	return filepath.Join(stateDir, controllerSecretName)
}

func configuredSecret(cfg map[string]any) string {
	secret, _ := cfg["secret"].(string)
	return secret
}

func controllerEnabled(cfg map[string]any) bool {
	main, tls := controllerEndpoints(cfg)
	return main != "" || tls != ""
}

func readControllerSecret(stateDir string) (string, bool, error) {
	path := controllerSecretPath(stateDir)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read controller secret %q: %w", path, err)
	}
	secret := strings.TrimSpace(string(data))
	if secret == "" {
		return "", false, nil
	}
	return secret, true, nil
}

func writeControllerSecret(stateDir, secret string) (string, error) {
	path := controllerSecretPath(stateDir)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create controller secret directory %q: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("create controller secret in %q: %w", dir, err)
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return "", fmt.Errorf("set controller secret mode on %q: %w", tmpPath, err)
	}
	if _, err := tmp.WriteString(secret); err != nil {
		return "", fmt.Errorf("write controller secret %q: %w", tmpPath, err)
	}
	if err := tmp.Sync(); err != nil {
		return "", fmt.Errorf("sync controller secret %q: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close controller secret %q: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return "", fmt.Errorf("replace controller secret %q: %w", path, err)
	}
	committed = true
	return path, nil
}

func ensureServeControllerSecret(cmd *cobra.Command, stateDir string, cfg map[string]any, fallback string) (string, error) {
	if configuredSecret(cfg) == "" && controllerEnabled(cfg) {
		secret, ok, err := readControllerSecret(stateDir)
		if err != nil {
			return "", err
		}
		if ok {
			cfg["secret"] = secret
			return secret, nil
		}
	}
	secret, generated, err := ensureControllerSecret(cfg, fallback)
	if err != nil {
		return "", err
	}
	if !generated {
		return secret, nil
	}
	path, err := writeControllerSecret(stateDir, secret)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "cx serve: generated controller secret in %s\n", path)
	return secret, nil
}

func ensureRunControllerSecret(cmd *cobra.Command, stateDir string, cfg map[string]any) (string, error) {
	secret, generated, err := ensureControllerSecret(cfg, "")
	if err != nil {
		return "", err
	}
	if !generated {
		return secret, nil
	}
	if stderrIsTerminal() {
		logControllerSecret(cmd, "run", secret)
		return secret, nil
	}
	path, err := writeControllerSecret(stateDir, secret)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "cx run: generated controller secret in %s\n", path)
	return secret, nil
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
