package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// LastGoodPath returns the path of the last-known-good companion file for an
// active configuration.
func LastGoodPath(path string) string {
	return path + ".last-good"
}

// Write validates cfg, preserves the current file as last-good, and atomically
// replaces path with a 0600 file.
func Write(path string, cfg map[string]any) error {
	data, err := Marshal(cfg)
	if err != nil {
		return err
	}
	return WriteYAML(path, data)
}

// WriteAtomic is an alias for Write.
func WriteAtomic(path string, cfg map[string]any) error {
	return Write(path, cfg)
}

// WriteYAML validates a YAML document, preserves the current active file as
// last-good, and atomically writes the new document with mode 0600.
func WriteYAML(path string, data []byte) error {
	if _, err := Parse(data); err != nil {
		return fmt.Errorf("refuse to write invalid config %q: %w", path, err)
	}

	if previous, err := os.ReadFile(path); err == nil {
		if _, parseErr := Parse(previous); parseErr == nil {
			if err := writeAtomic(LastGoodPath(path), previous, 0o600); err != nil {
				return fmt.Errorf("save last-good config: %w", err)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read current config %q: %w", path, err)
	}

	if err := writeAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("write config %q: %w", path, err)
	}
	return nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory %q: %w", dir, err)
	}

	prefix := "." + filepath.Base(path) + ".tmp-"
	tmp, err := os.CreateTemp(dir, prefix)
	if err != nil {
		return fmt.Errorf("create temporary file in %q: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	if err := tmp.Chmod(mode); err != nil {
		return fmt.Errorf("set mode on %q: %w", tmpPath, err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write %q: %w", tmpPath, err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync %q: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %q: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace %q: %w", path, err)
	}

	// Best effort: persist the directory entry as well. Some platforms do not
	// support syncing directories.
	if dirHandle, err := os.Open(dir); err == nil {
		_ = dirHandle.Sync()
		_ = dirHandle.Close()
	}
	return nil
}
