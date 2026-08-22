// Package clean is the defect-free counterpart of ../sixfamilies: the same
// six shapes, each written the way its analyzer family wants them, so a run
// over it must report nothing at all.
package clean

import (
	"errors"
	"os"
	"strings"
)

// WriteMarker returns the error os.WriteFile reports.
func WriteMarker(path string) error {
	return os.WriteFile(path, []byte("marker\n"), 0o644)
}

// Count returns how many items it was given.
func Count(items []string) int {
	return len(items)
}

// Trim strips the leading and trailing a's from s.
func Trim(s string) string {
	return strings.Trim(s, "a")
}

// Enabled renders flag as an on/off word.
func Enabled(flag bool) string {
	if flag {
		return "on"
	}
	return "off"
}

// ErrInput returns the error a caller reports for unusable input.
func ErrInput() error {
	return errors.New("bad input")
}
