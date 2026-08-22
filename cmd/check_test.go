package cmd

import (
	"errors"
	"testing"

	"github.com/johnrichter/claude-shared-tooling/go/toolchain"
)

// TestRunCheck_ErrUnsupportedCheckMapsToExit80 is the unit test on the error
// mapping the task calls for: runCheck must test toolchain.Run's error
// against toolchain.ErrUnsupportedCheck ahead of the generic internal-fault
// branch (reasoning cue: cmd/check.go's Run-error handling used to read
// every error as an internal fault, exit 90). Runs against a real rust
// target and the vet check -- cargo has no vet equivalent, so toolchain.Run
// returns an error wrapping ErrUnsupportedCheck without needing cargo on
// PATH (the adapter rejects the check before invoking any tool).
func TestRunCheck_ErrUnsupportedCheckMapsToExit80(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"vet", "--language", "rust", "--dir", ".", "--log-dir", t.TempDir()})
	root.SetOut(new(discard))
	_, err := root.ExecuteC()
	var ee *exitError
	if !errors.As(err, &ee) {
		t.Fatalf("ExecuteC() error = %v (%T), want *exitError", err, err)
	}
	if ee.code != 80 {
		t.Fatalf("exitError.code = %d, want 80 (clikit unsupported)", ee.code)
	}
}

// TestRunCheck_UnrelatedErrorDoesNotMapToUnsupported guards the other side
// of the branch: an error from toolchain.Run that does not wrap
// ErrUnsupportedCheck (an unregistered --language, today's only other
// failure mode surfaced) must still fall through to the internal-fault
// class (exit 90), not be misrouted to unsupported/80.
func TestRunCheck_UnrelatedErrorDoesNotMapToUnsupported(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"vet", "--language", "cobol", "--dir", t.TempDir()})
	root.SetOut(new(discard))
	_, err := root.ExecuteC()
	var ee *exitError
	if !errors.As(err, &ee) {
		t.Fatalf("ExecuteC() error = %v (%T), want *exitError", err, err)
	}
	if ee.code != 90 {
		t.Fatalf("exitError.code = %d, want 90 (unrelated toolchain.Run error must stay internal)", ee.code)
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
