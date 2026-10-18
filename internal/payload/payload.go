// Package payload reads and writes the optional tar payload appended to a cx
// executable.
package payload

import (
	"archive/tar"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	// Magic marks the end of a payload trailer.
	Magic = "CXPAYLD1"
	// TrailerSize is the size of the length + magic trailer.
	TrailerSize = 16
)

var magicBytes = []byte(Magic)

// Read returns the files stored in the payload appended to exePath. ok is
// false when the executable has no payload.
func Read(exePath string) (files map[string][]byte, ok bool, err error) {
	file, err := os.Open(exePath)
	if err != nil {
		return nil, false, fmt.Errorf("open executable %q: %w", exePath, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, false, fmt.Errorf("stat executable %q: %w", exePath, err)
	}
	start, found, err := locate(file, info.Size())
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}

	files = make(map[string][]byte)
	section := io.NewSectionReader(file, start, info.Size()-TrailerSize-start)
	reader := tar.NewReader(section)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, true, fmt.Errorf("read payload tar: %w", err)
		}
		switch header.Typeflag {
		case tar.TypeReg, tar.TypeRegA:
			// Continue below.
		case tar.TypeDir:
			continue
		default:
			return nil, true, fmt.Errorf("unsupported payload entry %q (type %d)", header.Name, header.Typeflag)
		}

		name, err := cleanTarName(header.Name)
		if err != nil {
			return nil, true, err
		}
		if _, exists := files[name]; exists {
			return nil, true, fmt.Errorf("duplicate payload entry %q", name)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			return nil, true, fmt.Errorf("read payload entry %q: %w", name, err)
		}
		files[name] = data
	}
	return files, true, nil
}

// Write copies srcExe to dstExe, removes an existing payload, appends files as
// a tar payload, and leaves dstExe executable and private (mode 0700).
func Write(srcExe, dstExe string, files map[string][]byte) error {
	if runtime.GOOS == "windows" {
		if err := rejectRunningExecutable(dstExe); err != nil {
			return err
		}
	}

	src, err := os.Open(srcExe)
	if err != nil {
		return fmt.Errorf("open source executable %q: %w", srcExe, err)
	}
	defer src.Close()

	info, err := src.Stat()
	if err != nil {
		return fmt.Errorf("stat source executable %q: %w", srcExe, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source executable %q is not a regular file", srcExe)
	}

	baseEnd, found, err := locate(src, info.Size())
	if err != nil {
		return fmt.Errorf("inspect source executable %q: %w", srcExe, err)
	}
	if !found {
		baseEnd = info.Size()
	}

	dir := filepath.Dir(dstExe)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create destination directory %q: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(dstExe)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary executable in %q: %w", dir, err)
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := io.CopyN(tmp, src, baseEnd); err != nil {
		return fmt.Errorf("copy executable %q: %w", srcExe, err)
	}
	// Windows refuses to replace a file while any handle is open, including
	// src when dstExe is the same path. All source reads are complete here.
	if err := src.Close(); err != nil {
		return fmt.Errorf("close source executable %q: %w", srcExe, err)
	}

	entries := make([]payloadEntry, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	for name, data := range files {
		clean, err := cleanTarName(name)
		if err != nil {
			return err
		}
		if _, duplicate := seen[clean]; duplicate {
			return fmt.Errorf("duplicate payload entry %q after path cleanup", clean)
		}
		seen[clean] = struct{}{}
		entries = append(entries, payloadEntry{name: clean, data: data})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })

	counter := &countingWriter{writer: tmp}
	tarWriter := tar.NewWriter(counter)
	for _, entry := range entries {
		header := &tar.Header{
			Name:     entry.name,
			Mode:     0o644,
			Size:     int64(len(entry.data)),
			Typeflag: tar.TypeReg,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("write payload header %q: %w", entry.name, err)
		}
		if _, err := tarWriter.Write(entry.data); err != nil {
			return fmt.Errorf("write payload entry %q: %w", entry.name, err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		return fmt.Errorf("finish payload tar: %w", err)
	}

	var trailer [TrailerSize]byte
	binary.LittleEndian.PutUint64(trailer[:8], counter.count)
	copy(trailer[8:], magicBytes)
	if _, err := tmp.Write(trailer[:]); err != nil {
		return fmt.Errorf("write payload trailer: %w", err)
	}
	if err := tmp.Chmod(0o700); err != nil {
		return fmt.Errorf("set executable mode on %q: %w", tmpPath, err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync executable %q: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close executable %q: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, dstExe); err != nil {
		return fmt.Errorf("replace executable %q: %w", dstExe, err)
	}
	committed = true
	return nil
}

func rejectRunningExecutable(dstExe string) error {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	dstInfo, err := os.Stat(dstExe)
	if err != nil {
		return nil
	}
	exeInfo, err := os.Stat(exe)
	if err != nil {
		return nil
	}
	if os.SameFile(dstInfo, exeInfo) {
		return fmt.Errorf("refusing to replace the running executable %q on Windows; choose a different -o", dstExe)
	}
	return nil
}

// locate inspects the trailer at the end of a file. When a payload is present
// it returns the offset where the tar section starts.
func locate(file *os.File, size int64) (int64, bool, error) {
	if size < TrailerSize {
		return 0, false, nil
	}
	var trailer [TrailerSize]byte
	if _, err := file.ReadAt(trailer[:], size-TrailerSize); err != nil {
		return 0, false, fmt.Errorf("read executable trailer: %w", err)
	}
	if !bytes.Equal(trailer[8:], magicBytes) {
		return 0, false, nil
	}
	length := binary.LittleEndian.Uint64(trailer[:8])
	maxLength := uint64(size - TrailerSize)
	if length > maxLength {
		return 0, false, fmt.Errorf("corrupt payload: length %d exceeds executable size", length)
	}
	return size - TrailerSize - int64(length), true, nil
}

func cleanTarName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("payload entry has an empty name")
	}
	name = strings.ReplaceAll(name, "\\", "/")
	if path.IsAbs(name) {
		return "", fmt.Errorf("payload entry %q is absolute", name)
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("payload entry %q escapes the payload root", name)
	}
	return clean, nil
}

type payloadEntry struct {
	name string
	data []byte
}

type countingWriter struct {
	writer io.Writer
	count  uint64
}

func (w *countingWriter) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	w.count += uint64(n)
	return n, err
}
