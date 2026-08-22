package pin

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// FileName is the pin file's fixed name and location: a check's own target
// directory, never a parent or a language-specific subdirectory.
const FileName = "mise.toml"

// misePins is the slice of mise.toml this package reads: its [tools] table,
// keyed by tool name ("go", "rust", "python") with the pinned version as
// its raw string. mise also accepts an array of versions per tool and a
// richer per-tool table; neither shape is a pin language-tools checks
// against, so decoding a mise.toml that uses one fails rather than silently
// picking a version out of it.
type misePins struct {
	Tools map[string]string `toml:"tools"`
}

// readPin reads dir's mise.toml and returns the version it pins for
// language. The returned error wraps os.ErrNotExist when the file itself is
// absent — the caller's cue to gate the check rather than treat this as an
// infrastructure fault — and is a plain error for a mise.toml that exists
// but won't parse, or that pins no version for language.
func readPin(dir, language string) (string, error) {
	path := filepath.Join(dir, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var pins misePins
	if _, err := toml.Decode(string(data), &pins); err != nil {
		return "", fmt.Errorf("pin: parse %s: %w", path, err)
	}
	version, ok := pins.Tools[language]
	if !ok || version == "" {
		return "", fmt.Errorf("pin: %s pins no version for %q", path, language)
	}
	return version, nil
}
