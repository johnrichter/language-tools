package pin

import (
	"context"
	"testing"

	"github.com/johnrichter/claude-shared-tooling/go/toolchain"
)

// TestRootMiseTomlSatisfiesOwnFloor is adversarial verification for the
// SC9c deliverable itself: it runs Check against the real repo-root
// mise.toml and go.mod (".."/".." from this package, i.e. the repo root),
// not a synthetic fixture. It fails loudly if the shipped pin ever drifts
// below the repo's own K8 floor (go.mod's "go 1.26.0" line), or if the
// pin file goes missing or stops parsing.
func TestRootMiseTomlSatisfiesOwnFloor(t *testing.T) {
	target := toolchain.Target{
		Dir:      "../..",
		Language: "go",
		Check:    toolchain.CheckBuild,
	}
	result, err := Check(context.Background(), []string{"language-tools", "build"}, target)
	if err != nil {
		t.Fatalf("Check on repo-root mise.toml returned infra error (missing/unparseable pin or go.mod?): %v", err)
	}
	if result != nil {
		t.Fatalf("Check on repo-root mise.toml returned gate_negative, want nil (pin must satisfy K8 floor): %+v", result)
	}
}

// TestRootMiseTomlPinExactlyMeetsFloor pins down the exact boundary case
// this deliverable sits on: pin == floor (both "1.26.0"), which SC9a's
// >= rule must accept. Guards against a future >  regression in
// compareVersions/floorFor being applied to the shipped pin.
func TestRootMiseTomlPinExactlyMeetsFloor(t *testing.T) {
	pin, err := readPin("../..", "go")
	if err != nil {
		t.Fatalf("readPin(repo root): %v", err)
	}
	floor, ok, err := floorFor("../..", "go")
	if err != nil {
		t.Fatalf("floorFor(repo root): %v", err)
	}
	if !ok {
		t.Fatalf("floorFor(repo root) reported no floor; expected go.mod's go directive")
	}
	if compareVersions(floor, pin) > 0 {
		t.Fatalf("repo-root pin %q sits below its own floor %q; SC9a requires pin >= floor", pin, floor)
	}
}
