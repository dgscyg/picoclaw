package fileutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomic_OverwritesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	if err := WriteFileAtomic(path, []byte("first"), 0o600); err != nil {
		t.Fatalf("first WriteFileAtomic() error = %v", err)
	}
	if err := WriteFileAtomic(path, []byte("second"), 0o600); err != nil {
		t.Fatalf("second WriteFileAtomic() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got, want := string(data), "second"; got != want {
		t.Fatalf("file content = %q, want %q", got, want)
	}
}
