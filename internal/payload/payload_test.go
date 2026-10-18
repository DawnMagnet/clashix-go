package payload

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeSource(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "cx-source")
	content := []byte("#!/bin/sh\necho source\n")
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadNonPackedBinary(t *testing.T) {
	dir := t.TempDir()
	src := writeSource(t, dir)
	files, ok, err := Read(src)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if ok {
		t.Fatal("Read reported a payload on a plain binary")
	}
	if files != nil {
		t.Fatalf("files = %#v, want nil", files)
	}
}

func TestWriteAndReadPayload(t *testing.T) {
	dir := t.TempDir()
	src := writeSource(t, dir)
	dst := filepath.Join(dir, "cx-packed")
	want := map[string][]byte{
		"config.yaml":  []byte("port: 7890\n"),
		"geoip.metadb": bytes.Repeat([]byte{1, 2, 3}, 100),
		"geosite.dat":  []byte("geosite"),
	}
	if err := Write(src, dst, want); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(dst)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Errorf("packed binary mode = %o, want 700", info.Mode().Perm())
		}
	}

	got, ok, err := Read(dst)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !ok {
		t.Fatal("Read did not find the payload")
	}
	if len(got) != len(want) {
		t.Fatalf("got %d files, want %d", len(got), len(want))
	}
	for name, data := range want {
		if !bytes.Equal(got[name], data) {
			t.Errorf("file %q = %q, want %q", name, got[name], data)
		}
	}

	sourceData, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	packedData, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(packedData, sourceData) {
		t.Error("packed binary does not preserve the source binary prefix")
	}
}

func TestRepackStripsOldPayload(t *testing.T) {
	dir := t.TempDir()
	src := writeSource(t, dir)
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")

	oldFiles := map[string][]byte{"config.yaml": []byte("old: true\n")}
	if err := Write(src, first, oldFiles); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	newFiles := map[string][]byte{"config.yaml": []byte("new: true\n")}
	if err := Write(first, second, newFiles); err != nil {
		t.Fatalf("repack Write: %v", err)
	}

	got, ok, err := Read(second)
	if err != nil || !ok {
		t.Fatalf("Read repacked = %v, %v; want ok", ok, err)
	}
	if string(got["config.yaml"]) != "new: true\n" {
		t.Errorf("config.yaml = %q, want new payload", got["config.yaml"])
	}
	if len(got) != 1 {
		t.Errorf("repacked payload has %d files, want 1", len(got))
	}

	secondData, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(secondData, []byte("old: true")) {
		t.Error("old payload leaked into the repacked binary")
	}
	if !bytes.Contains(secondData, []byte(Magic)) {
		t.Error("new binary is missing the payload trailer")
	}
}

func TestWriteRepackInPlace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cx")
	if err := Write(writeSource(t, dir), path, map[string][]byte{"one": []byte("1")}); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, path, map[string][]byte{"two": []byte("2")}); err != nil {
		t.Fatalf("in-place Write: %v", err)
	}
	got, ok, err := Read(path)
	if err != nil || !ok {
		t.Fatalf("Read = %v, %v; want ok", ok, err)
	}
	if _, exists := got["one"]; exists {
		t.Error("old file survived in-place repack")
	}
	if string(got["two"]) != "2" {
		t.Errorf("two = %q", got["two"])
	}
}

func TestWriteEmptyFiles(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "cx-empty")
	if err := Write(writeSource(t, dir), dst, nil); err != nil {
		t.Fatalf("Write: %v", err)
	}
	files, ok, err := Read(dst)
	if err != nil || !ok {
		t.Fatalf("Read = %v, %v; want ok", ok, err)
	}
	if len(files) != 0 {
		t.Errorf("files = %#v, want empty", files)
	}
}

func TestWriteRejectsUnsafeNames(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"../escape", "/absolute", ""} {
		t.Run(name, func(t *testing.T) {
			err := Write(writeSource(t, dir), filepath.Join(dir, "out"), map[string][]byte{name: []byte("x")})
			if err == nil {
				t.Fatal("Write accepted an unsafe name")
			}
		})
	}
}

func TestReadCorruptPayload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt")
	data := []byte("binary")
	var trailer [TrailerSize]byte
	binary.LittleEndian.PutUint64(trailer[:8], 1<<40)
	copy(trailer[8:], []byte(Magic))
	data = append(data, trailer[:]...)
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := Read(path); err == nil || ok {
		t.Fatalf("Read corrupt = %v, %v; want false with error", ok, err)
	}
}

func TestReadMissingFile(t *testing.T) {
	_, ok, err := Read(filepath.Join(t.TempDir(), "missing"))
	if err == nil || ok {
		t.Fatalf("Read missing = %v, %v; want error", ok, err)
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Logf("error is wrapped without os.PathError, which is acceptable: %v", err)
	}
}
