package result

import (
	"strings"
	"testing"

	"github.com/johnrichter/claude-shared-tooling/go/clikit"
	"github.com/johnrichter/claude-shared-tooling/go/toolchain"
)

// Adversarial: a real cargo diagnostic's Message can carry embedded
// newlines (multi-line note/help text some rustc lints attach to the JSON
// "message" field). FromRun passes d.Message straight into
// clikit.NewError/NewCaveat with no oneLine-style sanitization, unlike
// cmd/root.go's finishErr/finishUsage which now sanitize theirs. clikit
// rejects any control character in a diagnostic message, so this input
// makes FromRun fail entirely -- turning a legitimate gate_negative build
// failure into an internal-classification failure one layer up (checked
// by TestRunCheck_MultilineToolDiagnostic_DoesNotMisclassify below at the
// cmd layer, which this test's failure predicts).
func TestFromRun_MultilineDiagnosticMessage(t *testing.T) {
	diags := []toolchain.Diagnostic{{
		Severity: toolchain.SeverityError,
		Message:  "mismatched types\n  expected `i32`\n     found `&str`",
		File:     "src/main.rs",
		Line:     3,
	}}
	r, err := FromRun([]string{"language-tools", "build"}, run(clikit.StatusGateNegative, toolchain.Counts{Errors: 1}, diags, 0))
	if err != nil {
		t.Fatalf("FromRun rejected a multi-line tool diagnostic instead of sanitizing it: %v (a genuine build failure would be misreported as status=internal/exit=90 instead of gate_negative/20)", err)
	}
	if len(r.Errors) != 1 {
		t.Fatalf("errors = %d, want 1", len(r.Errors))
	}
	if strings.ContainsAny(r.Errors[0].Message, "\n\r\t") {
		t.Errorf("error message still carries raw control characters: %q", r.Errors[0].Message)
	}
}

// Adversarial: collapsing a multi-line message onto one line can push it past
// clikit's 4096-char line bound, and a message that is only whitespace/control
// characters collapses to empty. clikit rejects both, so FromRun must bound
// and floor the message rather than let construction fail and misreport the
// gate_negative build failure as internal one layer up.
func TestFromRun_OversizeAndEmptyDiagnosticMessages(t *testing.T) {
	cases := map[string]string{
		"oversize": strings.Repeat("mismatched types\n", 400), // collapses to ~6800 chars, one line
		"blank":    "\n\t\r ",                                 // collapses to empty, no File to fall back on
	}
	for name, msg := range cases {
		t.Run(name, func(t *testing.T) {
			diags := []toolchain.Diagnostic{{Severity: toolchain.SeverityError, Message: msg}}
			r, err := FromRun([]string{"language-tools", "build"}, run(clikit.StatusGateNegative, toolchain.Counts{Errors: 1}, diags, 0))
			if err != nil {
				t.Fatalf("FromRun rejected a %s tool diagnostic instead of sanitizing it: %v", name, err)
			}
			if len(r.Errors) != 1 {
				t.Fatalf("errors = %d, want 1", len(r.Errors))
			}
			if got := r.Errors[0].Message; got == "" || len(got) > 4096 {
				t.Errorf("sanitized message violates clikit line bound: len=%d", len(got))
			}
		})
	}
}
