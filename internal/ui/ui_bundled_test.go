//go:build ui

package ui

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestBundledAssetsExtract(t *testing.T) {
	if !Bundled() {
		t.Fatal("ui-tagged build did not report bundled assets")
	}
	if !strings.Contains(Version(), "metacubexd") {
		t.Errorf("panel version = %q", Version())
	}
	dir, err := Extract(t.TempDir())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	index, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatalf("read extracted index.html: %v", err)
	}
	if !strings.Contains(string(index), "MetaCubeXD") {
		t.Error("extracted index.html does not look like metacubexd")
	}

	matches := regexp.MustCompile(`(?:_nuxt/)[A-Za-z0-9._-]+`).FindAllString(string(index), -1)
	if len(matches) == 0 {
		t.Fatal("index.html does not reference a _nuxt asset")
	}
	for _, asset := range matches {
		if _, err := fs.Stat(bundled, asset); err == nil {
			return
		}
	}
	t.Fatalf("none of the index.html _nuxt assets exist in the embedded FS: %v", matches)
}
