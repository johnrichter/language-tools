package cmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/johnrichter/claude-shared-tooling/go/toolchain"
)

// TestRunCheck_ErrUnsupportedCheckMapsToExit80 is the unit test on the error
// mapping the task calls for: runCheck must test toolchain.Run's error
// against toolchain.ErrUnsupportedCheck ahead of the generic internal-fault
// branch (reasoning cue: cmd/check.go's Run-error handling used to read
// every error as an internal fault, exit 90). Runs against a real rust
// target and the vet check -- cargo has no vet equivalent, so toolchain.Run
// returns an error wrapping ErrUnsupportedCheck. The target dir is the
// checked-in pin fixture whose mise.toml pin an installed rustc satisfies,
// so the pin check ahead of toolchain.Run passes and the unsupported-check
// error still surfaces from toolchain.Run itself.
func TestRunCheck_ErrUnsupportedCheckMapsToExit80(t *testing.T) {
	if _, err := exec.LookPath("rustc"); err != nil {
		t.Skip("rustc not on PATH")
	}
	dir, err := filepath.Abs(filepath.Join("..", "internal", "pin", "testdata", "rust", "fixture1"))
	if err != nil {
		t.Fatal(err)
	}
	root := newRootCmd()
	root.SetArgs([]string{"vet", "--language", "rust", "--dir", dir, "--log-dir", t.TempDir()})
	root.SetOut(new(discard))
	_, err = root.ExecuteC()
	var ee *exitError
	if !errors.As(err, &ee) {
		t.Fatalf("ExecuteC() error = %v (%T), want *exitError", err, err)
	}
	if ee.code != 80 {
		t.Fatalf("exitError.code = %d, want 80 (clikit unsupported)", ee.code)
	}
}

// TestRunCheck_UnrelatedErrorDoesNotMapToUnsupported guards the other side
// of the branch: an error that does not wrap ErrUnsupportedCheck must still
// fall through to the internal-fault class (exit 90), not be misrouted to
// unsupported/80. The target dir pins "cobol" itself, so the pin check
// ahead of toolchain.Run passes readPin and floor validation, then fails
// resolving "cobol"'s toolchain version -- an infrastructure fault the
// internal class covers, surfacing ahead of toolchain.Run's own
// unregistered-language failure.
func TestRunCheck_UnrelatedErrorDoesNotMapToUnsupported(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mise.toml"), []byte("[tools]\ncobol = \"1.0.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := newRootCmd()
	root.SetArgs([]string{"vet", "--language", "cobol", "--dir", dir})
	root.SetOut(new(discard))
	_, err := root.ExecuteC()
	var ee *exitError
	if !errors.As(err, &ee) {
		t.Fatalf("ExecuteC() error = %v (%T), want *exitError", err, err)
	}
	if ee.code != 90 {
		t.Fatalf("exitError.code = %d, want 90 (unrelated pin/toolchain error must stay internal)", ee.code)
	}
}

// TestErrUnsupportedCheckSentinel_DoesNotMatchUnrelatedError is a narrow
// sentinel-matching unit test independent of the CLI plumbing: a plain,
// unwrapped error must never satisfy errors.Is against
// toolchain.ErrUnsupportedCheck.
func TestErrUnsupportedCheckSentinel_DoesNotMatchUnrelatedError(t *testing.T) {
	unrelated := errors.New("some other failure")
	if errors.Is(unrelated, toolchain.ErrUnsupportedCheck) {
		t.Fatal("errors.Is matched an unrelated error against toolchain.ErrUnsupportedCheck")
	}
}

// discard is a minimal io.Writer sink so cobra's own stdout emission (the
// clikit.Result JSON) doesn't clutter test output; the assertions here are
// on the returned error, not the emitted record.
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
