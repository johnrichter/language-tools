// Package sixfamilies plants exactly one defect from each analyzer family
// the driver registers, so a run over it proves every family reaches a
// diagnostic. Its clean counterpart (../clean) is the same six shapes
// written correctly.
package sixfamilies

import (
	"errors"
	"os"
	"strings"
)

// WriteMarker drops the error os.WriteFile returns: the errcheck family.
func WriteMarker(path string) {
	os.WriteFile(path, []byte("marker\n"), 0o644)
}

// Count overwrites its first assignment before anything reads it: the
// ineffassign family.
func Count(items []string) int {
	n := 1
	n = len(items)
	return n
}

// Trim passes a cutset that repeats a character: the SA family (SA1024).
func Trim(s string) string {
	return strings.Trim(s, "aa")
}

// Enabled compares against a bool constant: the S family (S1002).
func Enabled(flag bool) string {
	if flag == true {
		return "on"
	}
	return "off"
}

// ErrInput returns an error string that opens with a capital: the ST family
// (ST1005).
func ErrInput() error {
	return errors.New("Bad input")
}

// unusedHelper is never called: the U1000 family.
func unusedHelper() string {
	return "unused"
}

// unusedRecord is never instantiated: U1000 again, for a type rather than a
// function.
type unusedRecord struct{}
