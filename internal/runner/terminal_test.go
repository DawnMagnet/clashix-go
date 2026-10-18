package runner

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestShouldIsolateProcessGroup(t *testing.T) {
	regularPath := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(regularPath, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	regular, err := os.Open(regularPath)
	if err != nil {
		t.Fatal(err)
	}
	defer regular.Close()

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()

	tests := []struct {
		name  string
		stdin io.Reader
		want  bool
	}{
		{name: "nil", stdin: nil, want: true},
		{name: "non file", stdin: bytes.NewBuffer(nil), want: true},
		{name: "regular file", stdin: regular, want: true},
		{name: "non-terminal character device", stdin: devNull, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldIsolateProcessGroup(test.stdin); got != test.want {
				t.Errorf("shouldIsolateProcessGroup = %v, want %v", got, test.want)
			}
		})
	}
}
