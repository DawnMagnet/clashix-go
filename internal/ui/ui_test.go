package ui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestExtractWithoutAssets(t *testing.T) {
	if Bundled() {
		t.Skip("test binary was built with the ui tag")
	}
	if _, err := Extract(t.TempDir()); !errors.Is(err, ErrNotBundled) {
		t.Fatalf("Extract error = %v, want ErrNotBundled", err)
	}
}

func TestExtractFS(t *testing.T) {
	source := fstest.MapFS{
		"index.html":       {Data: []byte("<html>panel</html>")},
		"_nuxt/entry.js":   {Data: []byte("console.log('ok')")},
		"nested/file.json": {Data: []byte("{}")},
	}
	target := filepath.Join(t.TempDir(), "ui")
	if err := extractFS(source, target); err != nil {
		t.Fatalf("extractFS: %v", err)
	}
	for name, want := range map[string]string{
		"index.html":       "<html>panel</html>",
		"_nuxt/entry.js":   "console.log('ok')",
		"nested/file.json": "{}",
	} {
		got, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}
