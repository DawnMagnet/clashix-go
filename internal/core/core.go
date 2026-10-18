// Package core embeds the mihomo proxy engine behind the cx lifecycle
// interface.
package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/DawnMagnet/clashix-go/internal/config"
	"github.com/DawnMagnet/clashix-go/internal/payload"
	"github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/hub"
	"github.com/metacubex/mihomo/hub/executor"
	mihomolog "github.com/metacubex/mihomo/log"
	"github.com/sirupsen/logrus"
)

// Core starts and stops the embedded proxy engine.
type Core interface {
	Start(ctx context.Context, dir string, cfg []byte) error
	Stop() error
}

// Applier optionally supports reloading a running core.
type Applier interface {
	Apply(dir string, cfg []byte) error
}

// ErrAlreadyStarted is returned because mihomo keeps process-global state and
// can only be started once per process.
var ErrAlreadyStarted = errors.New("mihomo core already started in this process")

var (
	processMu      sync.Mutex
	processStarted bool
)

// DefaultReadyTimeout bounds startup readiness checks.
const DefaultReadyTimeout = 15 * time.Second

// GeodataNames lists the payload files cx installs into the state directory.
var GeodataNames = []string{
	"geoip.metadb",
	"geoip.dat",
	"geosite.dat",
	"GeoLite2-ASN.mmdb",
	"ASN.mmdb",
}

func init() {
	// mihomo's logging defaults to stdout; the service is expected to log to
	// stderr so stdout stays usable by child processes.
	logrus.SetOutput(os.Stderr)
}

// SetLogLevel sets mihomo's process-wide log level.
func SetLogLevel(level string) error {
	parsed, ok := mihomolog.LogLevelMapping[strings.ToLower(level)]
	if !ok {
		return fmt.Errorf("invalid log level %q (want debug, info, warning, error, or silent)", level)
	}
	mihomolog.SetLevel(parsed)
	return nil
}

// Mihomo is the in-process proxy core.
type Mihomo struct {
	mu      sync.Mutex
	started bool
}

var (
	_ Core    = (*Mihomo)(nil)
	_ Applier = (*Mihomo)(nil)
)

// NewMihomo returns a new core instance.
func NewMihomo() *Mihomo {
	return &Mihomo{}
}

// New returns the embedded mihomo core.
func New() Core {
	return NewMihomo()
}

// Validate parses and validates cfg using dir as mihomo's home directory. It
// does not apply the configuration or start listeners.
func Validate(dir string, cfg []byte) error {
	if err := prepareDir(dir); err != nil {
		return err
	}
	if err := installGeodata(dir); err != nil {
		return err
	}
	constant.SetHomeDir(dir)

	prepared, err := prepareConfig(cfg)
	if err != nil {
		return err
	}
	if _, err := executor.ParseWithBytes(prepared); err != nil {
		return fmt.Errorf("validate mihomo config: %w", err)
	}
	return nil
}

// Start validates and applies cfg. Only one Start is allowed per process.
func (m *Mihomo) Start(ctx context.Context, dir string, cfg []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return ErrAlreadyStarted
	}
	if !claimProcess() {
		return ErrAlreadyStarted
	}
	if err := m.startLocked(ctx, dir, cfg); err != nil {
		releaseProcess()
		return err
	}
	m.started = true
	return nil
}

func (m *Mihomo) startLocked(ctx context.Context, dir string, cfg []byte) error {
	if err := prepareDir(dir); err != nil {
		return err
	}
	if err := installGeodata(dir); err != nil {
		return err
	}
	constant.SetHomeDir(dir)

	prepared, err := prepareConfig(cfg)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := hub.Parse(prepared); err != nil {
		return fmt.Errorf("start mihomo core: %w", err)
	}
	return nil
}

// Apply replaces the running configuration. Parse failures leave the previous
// configuration active.
func (m *Mihomo) Apply(dir string, cfg []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started {
		return fmt.Errorf("apply mihomo config: core is not running")
	}
	if dir != "" {
		if err := prepareDir(dir); err != nil {
			return err
		}
		if err := installGeodata(dir); err != nil {
			return err
		}
		constant.SetHomeDir(dir)
	}

	prepared, err := prepareConfig(cfg)
	if err != nil {
		return err
	}
	if err := hub.Parse(prepared); err != nil {
		return fmt.Errorf("apply mihomo config: %w", err)
	}
	return nil
}

// Stop closes listeners and tunnels. It is safe to call more than once, but a
// stopped core cannot be started again in the same process.
func (m *Mihomo) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started {
		return nil
	}
	m.started = false

	// Shutdown parses an empty configuration, which emits info logs. Keep the
	// configured level unless it is more verbose than warning.
	previousLevel := mihomolog.Level()
	shutdownLevel := mihomolog.WARNING
	if previousLevel > shutdownLevel {
		shutdownLevel = previousLevel
	}
	mihomolog.SetLevel(shutdownLevel)
	defer mihomolog.SetLevel(previousLevel)

	// Applying an empty configuration closes the HTTP, SOCKS, mixed, tunnel,
	// TUN, and controller listeners through mihomo's normal reload path.
	empty, err := executor.ParseWithBytes([]byte("{}\n"))
	if err == nil {
		empty.General.LogLevel = shutdownLevel
		hub.ApplyConfig(empty)
	} else {
		err = fmt.Errorf("prepare mihomo shutdown: %w", err)
	}
	executor.Shutdown()
	return err
}

func claimProcess() bool {
	processMu.Lock()
	defer processMu.Unlock()
	if processStarted {
		return false
	}
	processStarted = true
	return true
}

func releaseProcess() {
	processMu.Lock()
	defer processMu.Unlock()
	processStarted = false
}

func prepareDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("core state directory is empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create core state directory %q: %w", dir, err)
	}
	return nil
}

func prepareConfig(cfg []byte) ([]byte, error) {
	parsed, err := config.Parse(cfg)
	if err != nil {
		return nil, fmt.Errorf("parse mihomo config: %w", err)
	}
	config.ApplyGeoXDefaults(parsed)
	data, err := config.Marshal(parsed)
	if err != nil {
		return nil, fmt.Errorf("encode mihomo config: %w", err)
	}
	return data, nil
}

func installGeodata(dir string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current executable for geodata: %w", err)
	}
	files, ok, err := payload.Read(exe)
	if err != nil {
		return fmt.Errorf("read embedded geodata: %w", err)
	}
	if !ok {
		return nil
	}
	return writePayloadGeodata(dir, files)
}

func writePayloadGeodata(dir string, files map[string][]byte) error {
	for _, name := range GeodataNames {
		data, ok := files[name]
		if !ok {
			continue
		}
		if err := writeStateFile(filepath.Join(dir, name), data, 0o600); err != nil {
			return err
		}
		// mihomo looks for ASN.mmdb while the published payload uses the
		// upstream GeoLite2 name.
		if name == "GeoLite2-ASN.mmdb" {
			if err := writeStateFile(filepath.Join(dir, "ASN.mmdb"), data, 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeStateFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary geodata file in %q: %w", dir, err)
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(mode); err != nil {
		return fmt.Errorf("set geodata mode on %q: %w", tmpPath, err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write geodata %q: %w", tmpPath, err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync geodata %q: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close geodata %q: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace geodata %q: %w", path, err)
	}
	committed = true
	return nil
}

// WaitReady waits until address accepts TCP connections or timeout expires.
func WaitReady(ctx context.Context, address string, timeout time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		timeout = DefaultReadyTimeout
	}
	deadline := time.Now().Add(timeout)
	dialer := net.Dialer{Timeout: 250 * time.Millisecond}
	var lastErr error
	for {
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wait for %s: %w", address, lastErr)
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
