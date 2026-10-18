package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/DawnMagnet/clashix-go/internal/config"
	"github.com/DawnMagnet/clashix-go/internal/core"
	"github.com/DawnMagnet/clashix-go/internal/payload"
	"github.com/DawnMagnet/clashix-go/internal/selection"
	"github.com/spf13/cobra"
)

// ExitError carries a child process exit code through Cobra. The main package
// turns it into os.Exit without printing an error message.
type ExitError struct {
	Code int
}

// Error implements error.
func (e *ExitError) Error() string {
	return fmt.Sprintf("child exited with status %d", e.Code)
}

var (
	newCore        = core.New
	validateConfig = core.Validate
)

const defaultUIAddress = "127.0.0.1:9090"

type configOptions struct {
	configPath         string
	subscriptions      []string
	subFiles           []string
	overrides          []string
	selects            []string
	port               int
	socksPort          int
	mixedPort          int
	redirPort          int
	tproxyPort         int
	allowLAN           bool
	mode               string
	logLevel           string
	externalController string
	secret             string

	selection    []string
	selectionSet bool
	overrideDir  string
}

func addConfigFlags(cmd *cobra.Command, opts *configOptions) {
	flags := cmd.Flags()
	flags.StringVar(&opts.configPath, "config", "", "configuration file to load")
	flags.StringArrayVar(&opts.subscriptions, "sub", nil, "subscription URL (repeatable)")
	flags.StringArrayVar(&opts.subFiles, "sub-file", nil, "file with subscription URLs, one per line (repeatable)")
	flags.StringArrayVar(&opts.overrides, "override", nil, "local override YAML file (repeatable, applied in order)")
	flags.StringArrayVar(&opts.selects, "select", nil, "select a node (repeatable; PATTERN or GROUP=PATTERN)")
	flags.IntVar(&opts.port, "port", 0, "override the HTTP proxy port (0 disables it)")
	flags.IntVar(&opts.socksPort, "socks-port", 0, "override the SOCKS proxy port (0 disables it)")
	flags.IntVar(&opts.mixedPort, "mixed-port", 0, "override the mixed proxy port (0 disables it)")
	flags.IntVar(&opts.redirPort, "redir-port", 0, "override the redir proxy port (0 disables it)")
	flags.IntVar(&opts.tproxyPort, "tproxy-port", 0, "override the tproxy port (0 disables it)")
	flags.BoolVar(&opts.allowLAN, "allow-lan", false, "override allow-lan (--allow-lan=false forces it off)")
	flags.StringVar(&opts.mode, "mode", "", "override the proxy mode")
	flags.StringVar(&opts.logLevel, "log-level", "", "override the log level")
	flags.StringVar(&opts.externalController, "external-controller", "", "override the controller address")
	flags.StringVar(&opts.secret, "secret", "", "override the controller secret")
}

func (opts *configOptions) overlay(cmd *cobra.Command) config.Overlay {
	changed := cmd.Flags().Changed
	var overlay config.Overlay
	if changed("port") {
		overlay.Port = config.IntPtr(opts.port)
	}
	if changed("socks-port") {
		overlay.SocksPort = config.IntPtr(opts.socksPort)
	}
	if changed("mixed-port") {
		overlay.MixedPort = config.IntPtr(opts.mixedPort)
	}
	if changed("redir-port") {
		overlay.RedirPort = config.IntPtr(opts.redirPort)
	}
	if changed("tproxy-port") {
		overlay.TProxyPort = config.IntPtr(opts.tproxyPort)
	}
	if changed("allow-lan") {
		overlay.AllowLAN = config.BoolPtr(opts.allowLAN)
	}
	if changed("mode") {
		overlay.Mode = config.StringPtr(opts.mode)
	}
	if changed("log-level") {
		overlay.LogLevel = config.StringPtr(opts.logLevel)
	}
	if changed("external-controller") {
		overlay.ExternalController = config.StringPtr(opts.externalController)
	}
	if changed("secret") {
		overlay.Secret = config.StringPtr(opts.secret)
	} else if secret := os.Getenv("CX_SECRET"); secret != "" {
		overlay.Secret = config.StringPtr(secret)
	}
	return overlay
}

func addUIFlag(cmd *cobra.Command, enabled *bool, address *string) {
	flags := cmd.Flags()
	flags.BoolVar(enabled, "ui", false, "enable the controller and web panel (--ui=false disables it)")
	flags.StringVar(address, "ui-addr", defaultUIAddress, "controller and web panel address (implies --ui)")
}

func (opts *configOptions) validatePorts(cmd *cobra.Command) error {
	changed := cmd.Flags().Changed
	ports := []struct {
		name  string
		value int
	}{
		{"port", opts.port},
		{"socks-port", opts.socksPort},
		{"mixed-port", opts.mixedPort},
		{"redir-port", opts.redirPort},
		{"tproxy-port", opts.tproxyPort},
	}
	for _, port := range ports {
		if changed(port.name) && (port.value < 0 || port.value > 65535) {
			return fmt.Errorf("--%s must be between 0 and 65535", port.name)
		}
	}
	return nil
}

// prepare expands environment and file based sources. CLI flags win over the
// environment; files are read only once per command.
func (opts *configOptions) prepare(cmd *cobra.Command) error {
	subscriptions := append([]string(nil), opts.subscriptions...)
	for _, path := range opts.subFiles {
		urls, mode, err := readSubscriptionFile(path)
		if err != nil {
			return err
		}
		if runtime.GOOS != "windows" && mode.Perm()&0o044 != 0 {
			fmt.Fprintf(cmd.ErrOrStderr(), "WARNING: cx: subscription file %q is group or world readable; restrict it with chmod 600\n", path)
		}
		subscriptions = append(subscriptions, urls...)
	}
	if value := strings.TrimSpace(os.Getenv("CX_SUB")); value != "" {
		subscriptions = append(subscriptions, strings.Fields(value)...)
	}
	opts.subscriptions = uniqueStrings(subscriptions)

	if cmd.Flags().Changed("select") {
		opts.selection = trimSpecs(opts.selects)
		opts.selectionSet = true
	} else if value := strings.TrimSpace(os.Getenv("CX_SELECT")); value != "" {
		opts.selection = splitSelectionEnv(value)
		opts.selectionSet = true
	}
	return nil
}

func readSubscriptionFile(path string) ([]string, os.FileMode, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, fmt.Errorf("inspect subscription file %q: %w", path, err)
	}
	if info.IsDir() {
		return nil, 0, fmt.Errorf("subscription file %q is a directory", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, fmt.Errorf("read subscription file %q: %w", path, err)
	}
	var urls []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		urls = append(urls, line)
	}
	return urls, info.Mode(), nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func trimSpecs(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func splitSelectionEnv(value string) []string {
	var specs []string
	for _, part := range strings.Split(value, ";") {
		if part = strings.TrimSpace(part); part != "" {
			specs = append(specs, part)
		}
	}
	return specs
}

// loadEffectiveConfig resolves the source order: embedded payload, explicit
// config file, subscriptions, fallback active config, override files, node
// selection, then the cx overlay.
func loadEffectiveConfig(ctx context.Context, opts *configOptions, overlay config.Overlay, usePayload bool, fallbackPath string) (map[string]any, error) {
	base := map[string]any{}
	haveBase := false

	if usePayload {
		embedded, ok, err := embeddedConfig()
		if err != nil {
			return nil, err
		}
		if ok {
			base = embedded
			haveBase = true
		}
	}

	if opts.configPath != "" {
		cfg, err := config.Load(opts.configPath)
		if err != nil {
			return nil, err
		}
		base = cfg
		haveBase = true
	}

	if len(opts.subscriptions) > 0 {
		fetched := make([]map[string]any, 0, len(opts.subscriptions))
		for _, url := range opts.subscriptions {
			cfg, err := config.Fetch(ctx, url)
			if err != nil {
				return nil, err
			}
			fetched = append(fetched, cfg)
		}
		subscription := config.MergeSubscriptions(fetched...)
		if haveBase {
			base = config.MergeBase(base, subscription)
		} else {
			base = subscription
		}
		haveBase = true
	}

	if !haveBase && fallbackPath != "" {
		cfg, err := config.Load(fallbackPath)
		switch {
		case err == nil:
			base = cfg
			haveBase = true
		case errors.Is(err, os.ErrNotExist):
			// No cached configuration yet.
		default:
			return nil, err
		}
	}

	if !haveBase {
		return nil, noConfigSourceError(fallbackPath)
	}

	overrideDir := opts.overrideDir
	if overrideDir == "" && fallbackPath != "" {
		overrideDir = filepath.Dir(fallbackPath)
	}
	if overrideDir != "" {
		override, ok, err := config.LoadOverride(filepath.Join(overrideDir, "override.yaml"), true)
		if err != nil {
			return nil, err
		}
		if ok {
			base = config.MergeOverride(base, override)
		}
	}
	for _, path := range opts.overrides {
		override, ok, err := config.LoadOverride(path, false)
		if err != nil {
			return nil, err
		}
		if ok {
			base = config.MergeOverride(base, override)
		}
	}
	if path := strings.TrimSpace(os.Getenv("CX_OVERRIDE")); path != "" {
		override, ok, err := config.LoadOverride(path, false)
		if err != nil {
			return nil, err
		}
		if ok {
			base = config.MergeOverride(base, override)
		}
	}

	specs := opts.selection
	if !opts.selectionSet {
		specs = config.SelectSpecs(base)
	}
	if err := selection.Validate(specs); err != nil {
		return nil, err
	}
	config.SetSelectSpecs(base, specs)
	config.ApplyOverlay(base, overlay)
	config.ApplyGeoXDefaults(base)
	return base, nil
}

func noConfigSourceError(cachedPath string) error {
	if cachedPath == "" {
		return fmt.Errorf("no configuration source found: provide --config FILE, --sub URL, or use a packed binary with an embedded config")
	}
	return fmt.Errorf("no configuration source found (checked embedded payload, --config, --sub, and cached config %q): provide --config FILE, --sub URL, run \"cx sub update\", or use a packed binary with an embedded config", cachedPath)
}

func embeddedConfig() (map[string]any, bool, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, false, fmt.Errorf("locate current executable: %w", err)
	}
	return embeddedConfigFrom(exe)
}

func embeddedConfigFrom(exe string) (map[string]any, bool, error) {
	files, ok, err := payload.Read(exe)
	if err != nil {
		return nil, false, fmt.Errorf("read embedded payload: %w", err)
	}
	if !ok {
		return nil, false, nil
	}
	data, ok := files["config.yaml"]
	if !ok {
		return nil, false, nil
	}
	cfg, err := config.Parse(data)
	if err != nil {
		return nil, false, fmt.Errorf("parse embedded config: %w", err)
	}
	return cfg, true, nil
}

func payloadFiles() ([]string, bool, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, false, fmt.Errorf("locate current executable: %w", err)
	}
	return payloadFilesFrom(exe)
}

func payloadFilesFrom(exe string) ([]string, bool, error) {
	files, ok, err := payload.Read(exe)
	if err != nil {
		return nil, false, fmt.Errorf("read embedded payload: %w", err)
	}
	if !ok {
		return nil, false, nil
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, true, nil
}

func describePayload(names []string, embedded bool) string {
	if !embedded {
		return "payload: none"
	}
	if len(names) == 0 {
		return "payload: embedded (no files)"
	}
	return "payload: embedded (" + strings.Join(names, ", ") + ")"
}

func defaultCachedConfigPath() (string, error) {
	dir, err := config.DefaultDir()
	if err != nil {
		return "", err
	}
	return config.DefaultConfigPath(dir), nil
}

func stateDir(dir string) (string, error) {
	if dir != "" {
		return dir, nil
	}
	return config.DefaultDir()
}

func resolveStateDir(dir string) (string, error) {
	resolved, err := stateDir(dir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(resolved, 0o700); err != nil {
		return "", fmt.Errorf("create state directory %q: %w", resolved, err)
	}
	return resolved, nil
}
