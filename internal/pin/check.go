package pin

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/johnrichter/claude-shared-tooling/go/clikit"
	"github.com/johnrichter/claude-shared-tooling/go/toolchain"
)

// Check verifies that target.Dir's toolchain for target.Language, on the
// route target.Check resolves, satisfies target.Dir's mise.toml pin, and
// that no floor target.Dir's own manifest declares sits above that pin.
//
// A nil *clikit.Result means the pin is satisfied: the caller proceeds to
// run target.Check's tool exactly as it would without this package. A
// non-nil result is a fully-formed class-20 (gate_negative) verdict, ready
// to emit in the tool's place — the pin check found grounds to stop before
// the tool ever ran.
//
// A returned error means the check itself could not be performed: mise.toml
// or the target's manifest exists but won't parse, or the subprocess-route
// tool isn't on PATH. That is an infrastructure fault, never a verdict —
// the one exception (a missing mise.toml) is reported as the gate_negative
// result SC9 requires, not as this kind of error.
func Check(ctx context.Context, command []string, target toolchain.Target) (*clikit.Result, error) {
	dir, language, check := target.Dir, target.Language, target.Check

	pinVersion, err := readPin(dir, language)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return gateNegative(command, dir, language, check,
				"gate_negative.pin.missing_mise_toml",
				fmt.Sprintf("%s: no %s — every routed check requires a toolchain pin", dir, FileName),
				fmt.Sprintf("add %s to %s with a [tools] entry pinning %s", FileName, dir, language),
			)
		}
		return nil, err
	}

	floorVersion, hasFloor, err := floorFor(dir, language)
	if err != nil {
		return nil, err
	}
	if hasFloor && compareVersions(floorVersion, pinVersion) > 0 {
		return gateNegative(command, dir, language, check,
			"gate_negative.pin.floor_above_pin",
			fmt.Sprintf("%s: declared floor %s for %s exceeds the %s pin %s", dir, floorVersion, language, FileName, pinVersion),
			fmt.Sprintf("raise the %s pin for %s in %s to at least %s", language, dir, FileName, floorVersion),
		)
	}

	route := routeFor(language, check)
	resolved, err := resolveVersion(ctx, language, route)
	if err != nil {
		return nil, err
	}
	if !satisfies(resolved, pinVersion) {
		return gateNegative(command, dir, language, check,
			"gate_negative.pin.version_below_pin",
			fmt.Sprintf("%s: resolved %s toolchain %s is below the %s pin %s", dir, language, resolved, FileName, pinVersion),
			fmt.Sprintf("install %s %s or newer, or lower the %s pin in %s", language, pinVersion, language, FileName),
		)
	}
	return nil, nil
}

// gateNegative builds the class-20 clikit.Result Check returns for a pin
// that fails to hold, carrying dir/language/check as diagnostic context so
// a caller reading the record — not just its exit code — can see what was
// checked without re-deriving it from the command line.
func gateNegative(command []string, dir, language string, check toolchain.Check, code, message, instruction string) (*clikit.Result, error) {
	diag, err := clikit.NewError(code, message, clikit.Manual(instruction), map[string]any{
		"dir":      dir,
		"language": language,
		"check":    string(check),
	})
	if err != nil {
		return nil, err
	}
	return clikit.NewGateNegative(command, nil, []clikit.Diagnostic{diag}, nil)
}
