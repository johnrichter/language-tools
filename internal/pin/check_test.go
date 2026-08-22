package pin

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/johnrichter/claude-shared-tooling/go/clikit"
	"github.com/johnrichter/claude-shared-tooling/go/toolchain"
)

var testCommand = []string{"language-tools", "build"}

// fixtureCase drives one of the twelve testdata fixtures (four per
// language) through Check, plus — for the two Go fixtures the plan calls
// out — a second run on the lint check, so the in-process route is proven
// in both directions using the same fixtures the subprocess route uses.
type fixtureCase struct {
	name      string
	language  string
	fixture   string
	check     toolchain.Check
	satisfied bool
}

var fixtureCases = []fixtureCase{
	// go: floor at/below pin, pin below the resolved toolchain -> satisfied.
	{"go/fixture1 build", "go", "fixture1", toolchain.CheckBuild, true},
	// go: floor above the pin -> gate_negative regardless of the toolchain.
	{"go/fixture2 build", "go", "fixture2", toolchain.CheckBuild, false},
	// go: no mise.toml -> gate_negative.
	{"go/fixture3 build", "go", "fixture3", toolchain.CheckBuild, false},
	// go: pin above the resolved toolchain -> gate_negative.
	{"go/fixture4 build", "go", "fixture4", toolchain.CheckBuild, false},
	// go, in-process route (lint): the same two directions again.
	{"go/fixture1 lint (in-process)", "go", "fixture1", toolchain.CheckLint, true},
	{"go/fixture4 lint (in-process)", "go", "fixture4", toolchain.CheckLint, false},

	{"rust/fixture1 build", "rust", "fixture1", toolchain.CheckBuild, true},
	{"rust/fixture2 build", "rust", "fixture2", toolchain.CheckBuild, false},
	{"rust/fixture3 build", "rust", "fixture3", toolchain.CheckBuild, false},
	{"rust/fixture4 build", "rust", "fixture4", toolchain.CheckBuild, false},

	{"python/fixture1 build", "python", "fixture1", toolchain.CheckBuild, true},
	{"python/fixture2 build", "python", "fixture2", toolchain.CheckBuild, false},
	{"python/fixture3 build", "python", "fixture3", toolchain.CheckBuild, false},
	{"python/fixture4 build", "python", "fixture4", toolchain.CheckBuild, false},
}

func TestCheckFixtures(t *testing.T) {
	for _, c := range fixtureCases {
		t.Run(c.name, func(t *testing.T) {
			target := toolchain.Target{
				Language: c.language,
				Check:    c.check,
				Dir:      filepath.Join("testdata", c.language, c.fixture),
			}
			result, err := Check(context.Background(), testCommand, target)
			if err != nil {
				t.Fatalf("Check(%+v): unexpected error: %v", target, err)
			}
			if c.satisfied {
				if result != nil {
					t.Fatalf("Check(%+v) = %+v, want nil (pin satisfied, tool should run)", target, result)
				}
				return
			}
			if result == nil {
				t.Fatalf("Check(%+v) = nil, want a gate_negative result", target)
			}
			if result.Status != clikit.StatusGateNegative {
				t.Errorf("Check(%+v).Status = %s, want %s", target, result.Status, clikit.StatusGateNegative)
			}
			if result.ExitCode != 20 {
				t.Errorf("Check(%+v).ExitCode = %d, want 20 (ai-shared-lib/go/clikit/status.go's gate_negative class)", target, result.ExitCode)
			}
			if len(result.Errors) == 0 {
				t.Errorf("Check(%+v).Errors is empty, want a governing diagnostic", target)
			}
		})
	}
}

// TestCheckFixtureOneIsNotAnExactMatch confirms SC9a's rule directly on the
// fixture the plan calls out: fixture one's pin ("1.20.0"-equivalent) and
// the resolved toolchain are never string-equal, yet the pin is satisfied.
func TestCheckFixtureOneIsNotAnExactMatch(t *testing.T) {
	dir := filepath.Join("testdata", "go", "fixture1")
	pin, err := readPin(dir, "go")
	if err != nil {
		t.Fatalf("readPin: %v", err)
	}
	resolved, err := resolveVersion(context.Background(), "go", toolchain.RouteSubprocess)
	if err != nil {
		t.Fatalf("resolveVersion: %v", err)
	}
	if pin == resolved {
		t.Fatalf("fixture invalid: pin %q must differ from the resolved toolchain %q", pin, resolved)
	}
	target := toolchain.Target{Language: "go", Check: toolchain.CheckBuild, Dir: dir}
	result, err := Check(context.Background(), testCommand, target)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result != nil {
		t.Fatalf("Check with pin %q below resolved %q = %+v, want nil", pin, resolved, result)
	}
}
