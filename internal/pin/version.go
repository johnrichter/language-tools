package pin

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"time"

	"github.com/johnrichter/claude-shared-tooling/go/sysops"
	"github.com/johnrichter/claude-shared-tooling/go/toolchain"
)

// versionProbeTimeout bounds a version subprocess (`go version`, `rustc
// --version`, `python3 --version`): a fixed, small budget, since a version
// flag returns immediately once the binary is found.
const versionProbeTimeout = 10 * time.Second

// routeFor reports where check resolves language's toolchain version. Go's
// lint check is the one in-process route this codebase has today — its
// analyzers run inside this binary, so there's no tool on PATH standing in
// for the toolchain — and every other language/check pair spawns a
// subprocess. This mirrors the routing the Go adapter itself applies
// (ai-shared-lib's toolchain.Adapter.Route); this package restates it
// rather than reading it from a registered adapter, since nothing exported
// from that package answers the question ahead of a run.
func routeFor(language string, check toolchain.Check) toolchain.Route {
	if language == "go" && check == toolchain.CheckLint {
		return toolchain.RouteInProcess
	}
	return toolchain.RouteSubprocess
}

// resolveVersion returns the dotted version string language's toolchain
// reports on route.
func resolveVersion(ctx context.Context, language string, route toolchain.Route) (string, error) {
	if route == toolchain.RouteInProcess {
		return resolveInProcessVersion(language)
	}
	return resolveSubprocessVersion(ctx, language)
}

// goRuntimeVersionRE pulls the dotted release out of runtime.Version(),
// e.g. "go1.26.5" -> "1.26.5".
var goRuntimeVersionRE = regexp.MustCompile(`^go(\d+(?:\.\d+){0,2})`)

// resolveInProcessVersion reads the Go release that built this binary.
// language is always "go" here: routeFor never answers RouteInProcess for
// any other language.
func resolveInProcessVersion(language string) (string, error) {
	if language != "go" {
		return "", fmt.Errorf("pin: no in-process version source for language %q", language)
	}
	v := runtime.Version()
	m := goRuntimeVersionRE.FindStringSubmatch(v)
	if m == nil {
		return "", fmt.Errorf("pin: could not parse a Go version out of runtime.Version() %q", v)
	}
	return m[1], nil
}

var (
	goVersionRE     = regexp.MustCompile(`go(\d+(?:\.\d+){0,2})`)
	rustcVersionRE  = regexp.MustCompile(`rustc (\d+(?:\.\d+){0,2})`)
	pythonVersionRE = regexp.MustCompile(`Python (\d+(?:\.\d+){0,2})`)
)

// resolveSubprocessVersion reads language's toolchain version from the tool
// the run resolves on PATH: the `go` binary for Go, `rustc` for Rust (the
// compiler `rust-version` names, not `cargo`'s own version), and `python3`
// (falling back to `python`) for Python.
func resolveSubprocessVersion(ctx context.Context, language string) (string, error) {
	switch language {
	case "go":
		return probeVersion(ctx, "go", []string{"version"}, goVersionRE)
	case "rust":
		return probeVersion(ctx, "rustc", []string{"--version"}, rustcVersionRE)
	case "python":
		version, err := probeVersion(ctx, "python3", []string{"--version"}, pythonVersionRE)
		if err != nil && errors.Is(err, exec.ErrNotFound) {
			return probeVersion(ctx, "python", []string{"--version"}, pythonVersionRE)
		}
		return version, err
	default:
		return "", fmt.Errorf("pin: no version probe for language %q", language)
	}
}

// probeVersion runs tool with args and pulls the first match of pattern out
// of its combined stdout and stderr. An error means tool could not be run
// at all (not on PATH, timed out) or its output didn't match pattern — an
// infrastructure fault, not a verdict about the pin.
func probeVersion(ctx context.Context, tool string, args []string, pattern *regexp.Regexp) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, versionProbeTimeout)
	defer cancel()
	res, err := sysops.Run(ctx, tool, args, sysops.Options{})
	if err != nil {
		return "", fmt.Errorf("pin: resolve %s version: %w", tool, err)
	}
	out := append(res.Stdout, res.Stderr...)
	m := pattern.FindSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("pin: could not read a version out of %s's output", tool)
	}
	return string(m[1]), nil
}
