package pin

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
)

func TestReadPin(t *testing.T) {
	version, err := readPin(filepath.Join("testdata", "go", "fixture1"), "go")
	if err != nil {
		t.Fatalf("readPin: %v", err)
	}
	if version != "1.20.0" {
		t.Errorf("readPin = %q, want 1.20.0", version)
	}
}

func TestReadPinNoMiseToml(t *testing.T) {
	_, err := readPin(filepath.Join("testdata", "go", "fixture3"), "go")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("readPin on a directory with no mise.toml: err = %v, want fs.ErrNotExist", err)
	}
}

func TestReadPinNoEntryForLanguage(t *testing.T) {
	_, err := readPin(filepath.Join("testdata", "go", "fixture1"), "rust")
	if err == nil {
		t.Fatal("readPin: want an error for a language mise.toml doesn't pin, got nil")
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("readPin: got fs.ErrNotExist, want a distinct parse-level error (the file exists)")
	}
}
