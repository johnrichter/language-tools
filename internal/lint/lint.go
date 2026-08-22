// Package lint runs Go's library analyzers over a target module inside this
// process and returns what they found as toolchain diagnostics. It exists
// because every analyzer in the set (errcheck, ineffassign, staticcheck's
// SA/S/ST families and U1000) ships as a Go library rather than a binary,
// so there is nothing for the toolchain's subprocess route to spawn: the
// module is loaded with go/packages and analyzed with go/analysis here.
//
// "In-process" bounds the analysis, not the whole run: loading a module
// still invokes the go command's list protocol, exactly as every go/analysis
// driver does. No lint binary is spawned, and none needs to be installed.
package lint

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/johnrichter/claude-shared-tooling/go/toolchain"
	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"
	"honnef.co/go/tools/unused"
)

// loadMode is what the analyzers need from go/packages: full syntax and
// type information for the target's packages and, because several analyzers
// exchange facts across package boundaries, for their dependencies too.
const loadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedImports |
	packages.NeedDeps |
	packages.NeedTypes |
	packages.NeedSyntax |
	packages.NeedTypesInfo |
	packages.NeedTypesSizes |
	packages.NeedModule

// Driver analyzes a Go module in-process. It carries no state of its own, so
// its zero value is ready to use.
type Driver struct{}

// New returns the Driver to hand to toolchain.NewGoAdapter.
func New() Driver { return Driver{} }

// Lint analyzes every package under target.Dir and returns one diagnostic
// per finding, ordered by position so a capped result is deterministic.
//
// A problem in the analyzed code — an analyzer finding, or a load/type
// error that stopped the analyzers from running — comes back as a
// Diagnostic. An error means the analysis itself could not be performed:
// the target holds no Go packages, the go command failed, ctx expired, or
// an analyzer faulted on a package that type-checked cleanly.
func (Driver) Lint(ctx context.Context, target toolchain.Target) ([]toolchain.Diagnostic, error) {
	// The analyzers report absolute paths; resolving the target the same way
	// is what lets each diagnostic name a path relative to it.
	root, err := filepath.Abs(target.Dir)
	if err != nil {
		return nil, fmt.Errorf("lint: resolve %s: %w", target.Dir, err)
	}
	pkgs, err := load(ctx, root)
	if err != nil {
		return nil, err
	}
	// An analyzer is only meaningful on code that type-checks, and the
	// driver skips every ill-typed package anyway — so a load or type error
	// is the whole finding, reported as an error diagnostic rather than
	// buried under one "analysis skipped" fault per analyzer.
	if diags := loadDiagnostics(pkgs, root); len(diags) > 0 {
		return diags, nil
	}

	graph, err := analyze(ctx, pkgs)
	if err != nil {
		return nil, err
	}

	var diags []toolchain.Diagnostic
	for _, act := range graph.Roots {
		if act.Err != nil {
			return nil, fmt.Errorf("lint: analyzer %s on package %s: %w", act.Analyzer.Name, act.Package.PkgPath, act.Err)
		}
		for _, d := range act.Diagnostics {
			pos := act.Package.Fset.Position(d.Pos)
			diags = append(diags, toolchain.Diagnostic{
				Severity: toolchain.SeverityWarning,
				Code:     act.Analyzer.Name,
				Message:  d.Message,
				File:     relative(root, pos.Filename),
				Line:     pos.Line,
			})
		}
		if act.Analyzer == unusedAnalyzer {
			diags = append(diags, readUnused(act.Result, root)...)
		}
	}
	sortDiagnostics(diags)
	return diags, nil
}

// load resolves every package under dir. The pattern is the module-wide
// "./..." the other checks use, and test files are left out: an unexported
// helper that only a test calls reads as unused in the non-test variant of
// its package, and reconciling that against the test variant is beyond what
// a plain go/analysis driver can express.
func load(ctx context.Context, dir string) ([]*packages.Package, error) {
	pkgs, err := packages.Load(&packages.Config{
		Context: ctx,
		Dir:     dir,
		Mode:    loadMode,
	}, "./...")
	if err != nil {
		return nil, fmt.Errorf("lint: load Go packages under %s: %w", dir, err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("lint: no Go packages under %s", dir)
	}
	return pkgs, nil
}

// loadDiagnostics reports the load and type errors of the target's own
// packages — a dependency's error surfaces here too, as the importing
// package's own "could not import" error.
func loadDiagnostics(pkgs []*packages.Package, dir string) []toolchain.Diagnostic {
	var diags []toolchain.Diagnostic
	for _, pkg := range pkgs {
		for _, e := range pkg.Errors {
			file, line := splitErrorPos(e.Pos)
			diags = append(diags, toolchain.Diagnostic{
				Severity: toolchain.SeverityError,
				Code:     "load",
				Message:  e.Msg,
				File:     relative(dir, file),
				Line:     line,
			})
		}
	}
	sortDiagnostics(diags)
	return diags
}

// analyze runs the analyzer set over pkgs, abandoning it if ctx expires
// first. checker.Analyze takes no context and cannot be interrupted, so a
// caller's deadline is honored by returning without it — acceptable in a
// CLI that exits once the run is over.
func analyze(ctx context.Context, pkgs []*packages.Package) (*checker.Graph, error) {
	type outcome struct {
		graph *checker.Graph
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		graph, err := checker.Analyze(Analyzers(), pkgs, nil)
		done <- outcome{graph, err}
	}()
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("lint: analysis did not finish: %w", ctx.Err())
	case out := <-done:
		if out.err != nil {
			return nil, fmt.Errorf("lint: run analyzers: %w", out.err)
		}
		return out.graph, nil
	}
}

// readUnused turns U1000's result into diagnostics. Alone in the set, that
// analyzer reports nothing through its pass: it returns the whole
// used/unused partition of the package's objects as its result, and only a
// driver that reads Result.Unused surfaces the family at all. The
// assertion is a formality: an analyzer returning something other than its
// declared result type has already failed the action above.
func readUnused(result any, dir string) []toolchain.Diagnostic {
	res, ok := result.(unused.Result)
	if !ok {
		return nil
	}
	diags := make([]toolchain.Diagnostic, 0, len(res.Unused))
	for _, obj := range res.Unused {
		diags = append(diags, toolchain.Diagnostic{
			Severity: toolchain.SeverityWarning,
			Code:     unusedAnalyzer.Name,
			Message:  fmt.Sprintf("%s %s is unused", obj.Kind, obj.Name),
			File:     relative(dir, obj.DisplayPosition.Filename),
			Line:     obj.DisplayPosition.Line,
		})
	}
	return diags
}

// sortDiagnostics orders findings by position, then by analyzer and text, so
// two runs over the same tree produce the same list — and, because the
// toolchain caps that list, the same capped verdict. Analyzers run in
// parallel and report in whatever order they finish.
func sortDiagnostics(diags []toolchain.Diagnostic) {
	sort.Slice(diags, func(i, j int) bool {
		a, b := diags[i], diags[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
}

// relative renders an analyzer's absolute file path relative to the target
// directory, leaving it absolute when it lies outside (a generated or
// module-cache file). An unpositioned finding keeps its empty path.
func relative(dir, file string) string {
	if file == "" {
		return ""
	}
	rel, err := filepath.Rel(dir, file)
	if err != nil || !filepath.IsLocal(rel) {
		return file
	}
	return rel
}

// splitErrorPos parses the "file:line:col" position a packages.Error
// carries as text. The position is empty for an error about the package as
// a whole (a failed `go list`, say), and column-less for some others.
func splitErrorPos(pos string) (file string, line int) {
	if pos == "" {
		return "", 0
	}
	parts := strings.Split(pos, ":")
	if len(parts) < 2 {
		return pos, 0
	}
	line, err := strconv.Atoi(parts[1])
	if err != nil {
		return parts[0], 0
	}
	return parts[0], line
}
