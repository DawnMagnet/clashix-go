package payload

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	if has, err := Has(src); err != nil || has {
		t.Fatalf("Has = %v, %v; want false without error", has, err)
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

	if has, err := Has(dst); err != nil || !has {
		t.Fatalf("Has = %v, %v; want true without error", has, err)
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

func TestWriteRejectsRunningExecutableOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("running executables can be replaced on Unix")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	err = Write(exe, exe, map[string][]byte{"config.yaml": []byte("new: true\n")})
	if err == nil {
		t.Fatal("Write replaced the running executable")
	}
	if !strings.Contains(err.Error(), "running executable") {
		t.Errorf("error = %v, want a clear running-executable error", err)
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

// TestUPXOverlayRoundTrip is a skip-if-no-upx test because CI does not install
// UPX. It checks that a UPX-compressed binary still accepts an appended payload
// and that a second UPX run cannot silently lose that payload.
func TestUPXOverlayRoundTrip(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("release builds skip UPX on darwin")
	}
	upx, err := exec.LookPath("upx")
	if err != nil {
		t.Skip("upx not installed; skipping UPX overlay round trip")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	base := filepath.Join(dir, "cx-test")
	copyExecutable(t, exe, base)
	if output, err := exec.Command(upx, "-q", "-1", base).CombinedOutput(); err != nil {
		t.Skipf("upx cannot compress the test binary: %v: %s", err, output)
	}

	packed := filepath.Join(dir, "cx-packed")
	want := map[string][]byte{"config.yaml": []byte("mixed-port: 0\n")}
	if err := Write(base, packed, want); err != nil {
		t.Fatalf("Write after UPX: %v", err)
	}
	got, ok, err := Read(packed)
	if err != nil || !ok || string(got["config.yaml"]) != string(want["config.yaml"]) {
		t.Fatalf("Read after UPX = %#v, %v, %v; want payload", got, ok, err)
	}
	if has, err := Has(packed); err != nil || !has {
		t.Fatalf("Has after UPX = %v, %v; want true", has, err)
	}

	before, err := os.ReadFile(packed)
	if err != nil {
		t.Fatal(err)
	}
	secondErr := exec.Command(upx, "-q", "-1", packed).Run()
	after, err := os.ReadFile(packed)
	if err != nil {
		t.Fatal(err)
	}
	if secondErr != nil && !bytes.Equal(before, after) {
		t.Error("failed UPX run modified the packed binary")
	}
	got, ok, err = Read(packed)
	if err != nil || !ok || string(got["config.yaml"]) != string(want["config.yaml"]) {
		t.Fatalf("payload after second UPX run = %#v, %v, %v; want preserved payload", got, ok, err)
	}
}

func copyExecutable(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}
