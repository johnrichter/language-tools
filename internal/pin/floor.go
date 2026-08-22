package pin

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/BurntSushi/toml"
	"golang.org/x/mod/modfile"
)

// floorFor reads the manifest language's own ecosystem uses to declare its
// minimum supported toolchain version, in dir. ok is false when dir holds
// no such manifest, or the manifest declares no floor — not a fixture case
// this package's callers hit today, but a target directory in general isn't
// guaranteed to carry one.
func floorFor(dir, language string) (version string, ok bool, err error) {
	switch language {
	case "go":
		return goFloor(dir)
	case "rust":
		return rustFloor(dir)
	case "python":
		return pythonFloor(dir)
	default:
		return "", false, nil
	}
}

// goFloor reads the `go` directive of dir/go.mod.
func goFloor(dir string) (string, bool, error) {
	path := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	mf, err := modfile.Parse(path, data, nil)
	if err != nil {
		return "", false, fmt.Errorf("pin: parse %s: %w", path, err)
	}
	if mf.Go == nil || mf.Go.Version == "" {
		return "", false, nil
	}
	return mf.Go.Version, true, nil
}

// cargoManifest is the one Cargo.toml field this package reads.
type cargoManifest struct {
	Package struct {
		RustVersion string `toml:"rust-version"`
	} `toml:"package"`
}

// rustFloor reads the `rust-version` field of dir/Cargo.toml's [package]
// table.
func rustFloor(dir string) (string, bool, error) {
	path := filepath.Join(dir, "Cargo.toml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var manifest cargoManifest
	if _, err := toml.Decode(string(data), &manifest); err != nil {
		return "", false, fmt.Errorf("pin: parse %s: %w", path, err)
	}
	if manifest.Package.RustVersion == "" {
		return "", false, nil
	}
	return manifest.Package.RustVersion, true, nil
}

// pyprojectManifest is the one pyproject.toml field this package reads.
type pyprojectManifest struct {
	Project struct {
		RequiresPython string `toml:"requires-python"`
	} `toml:"project"`
}

// requiresPythonFloorRE pulls the first dotted version out of a
// requires-python specifier — ">=3.10", "==3.12.1", or a bare "3.10" — and
// drops whichever comparison operator precedes it, plus any further
// comma-joined clause. requires-python states a minimum by convention, so
// its first clause is the floor this package compares against the pin;
// this package doesn't otherwise interpret PEP 440 version specifiers.
var requiresPythonFloorRE = regexp.MustCompile(`\d+(?:\.\d+){0,2}`)

// pythonFloor reads the `requires-python` field of dir/pyproject.toml's
// [project] table.
func pythonFloor(dir string) (string, bool, error) {
	path := filepath.Join(dir, "pyproject.toml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var manifest pyprojectManifest
	if _, err := toml.Decode(string(data), &manifest); err != nil {
		return "", false, fmt.Errorf("pin: parse %s: %w", path, err)
	}
	spec := manifest.Project.RequiresPython
	if spec == "" {
		return "", false, nil
	}
	version := requiresPythonFloorRE.FindString(spec)
	if version == "" {
		return "", false, fmt.Errorf("pin: could not read a version out of requires-python %q in %s", spec, path)
	}
	return version, true, nil
}
