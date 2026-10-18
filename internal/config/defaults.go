package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultDir returns the per-user cx state directory.
func DefaultDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(dir, "cx"), nil
}

// DefaultConfigPath returns the active configuration path inside a state
// directory.
func DefaultConfigPath(dir string) string {
	return filepath.Join(dir, "config.yaml")
}
