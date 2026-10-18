//go:build ui

package ui

import (
	"os"
	"path/filepath"
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
}
