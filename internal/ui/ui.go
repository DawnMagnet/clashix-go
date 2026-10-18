// Package ui embeds and extracts the optional built-in web dashboard.
package ui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

// ErrNotBundled is returned when this build does not include dashboard assets.
var ErrNotBundled = errors.New("web panel assets are not bundled in this build")

var (
	bundled        fs.FS
	bundledVersion string
)

//go:generate ../../scripts/fetch-ui.sh

// Bundled reports whether dashboard assets are embedded in this build.
func Bundled() bool {
	return bundled != nil
}

// Version returns the bundled dashboard version, or an empty string.
func Version() string {
	return bundledVersion
}

// Extract writes the bundled dashboard into dir/ui and returns that directory.
func Extract(dir string) (string, error) {
	if bundled == nil {
		return "", ErrNotBundled
	}
	target := filepath.Join(dir, "ui")
	if err := os.RemoveAll(target); err != nil {
		return "", fmt.Errorf("clear web panel directory %q: %w", target, err)
	}
	if err := extractFS(bundled, target); err != nil {
		return "", err
	}
	return target, nil
}

func extractFS(source fs.FS, target string) error {
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("create web panel directory %q: %w", target, err)
	}
	return fs.WalkDir(source, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		if !fs.ValidPath(name) || name != path.Clean(name) {
			return fmt.Errorf("invalid web panel asset path %q", name)
		}
		destination := filepath.Join(target, filepath.FromSlash(name))
		if entry.IsDir() {
			if err := os.MkdirAll(destination, 0o755); err != nil {
				return fmt.Errorf("create web panel directory %q: %w", destination, err)
			}
			return nil
		}
		data, err := fs.ReadFile(source, name)
		if err != nil {
			return fmt.Errorf("read web panel asset %q: %w", name, err)
		}
		if err := os.WriteFile(destination, data, 0o644); err != nil {
			return fmt.Errorf("write web panel asset %q: %w", destination, err)
		}
		return nil
	})
}
