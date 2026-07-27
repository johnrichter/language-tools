package result

import (
	"testing"

	"github.com/johnrichter/claude-shared-tooling/go/clikit"
	"github.com/johnrichter/claude-shared-tooling/go/toolchain"
)

func run(status clikit.Status, counts toolchain.Counts, diags []toolchain.Diagnostic, overflow int) *toolchain.RunResult {
	return &toolchain.RunResult{
		Tool:        "cargo",
		Language:    "rust",
		Status:      status,
		Counts:      counts,
		Diagnostics: diags,
		Overflow:    overflow,
		LogRef:      "/tmp/log.json",
		Impact:      toolchain.ImpactExecuted,
		DurationMS:  42,
	}
}

func TestFromRun_CleanSuccess(t *testing.T) {
	r, err := FromRun([]string{"language-tools", "build"}, run(clikit.StatusSuccess, toolchain.Counts{}, nil, 0))
	if err != nil {
		t.Fatalf("FromRun: %v", err)
	}
	if r.Status != clikit.StatusSuccess {
		t.Errorf("status = %q, want success", r.Status)
	}
	if r.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", r.ExitCode)
	}
	if len(r.Errors) != 0 || len(r.Caveats) != 0 {
		t.Errorf("clean success carries diagnostics: errors=%v caveats=%v", r.Errors, r.Caveats)
	}
}

// A success with a non-zero warning count must promote to caveats even
// though toolchain's own Status field says "success" — this is the one
// piece of classification logic this package adds on top of toolchain's
// verdict, and it must not regress into either silently swallowing the
// warning or misreporting it as a hard failure.
func TestFromRun_WarningsPromoteSuccessToCaveats(t *testing.T) {
	diags := []toolchain.Diagnostic{{Severity: toolchain.SeverityWarning, Message: "unused variable", File: "src/main.rs", Line: 2, Code: "unused_variables"}}
	r, err := FromRun([]string{"language-tools", "lint"}, run(clikit.StatusSuccess, toolchain.Counts{Warnings: 1}, diags, 0))
	if err != nil {
		t.Fatalf("FromRun: %v", err)
	}
	if r.Status != clikit.StatusCaveats {
		t.Fatalf("status = %q, want caveats", r.Status)
	}
	if r.ExitCode != clikit.StatusCaveats.ExitCode() {
		t.Errorf("exit code = %d, want %d", r.ExitCode, clikit.StatusCaveats.ExitCode())
	}
	if len(r.Caveats) != 1 {
		t.Fatalf("caveats = %d, want 1", len(r.Caveats))
	}
	if r.Caveats[0].Message == "" || r.Caveats[0].Context["tool_code"] != "unused_variables" {
		t.Errorf("caveat did not carry through diagnostic detail: %+v", r.Caveats[0])
	}
}

func TestFromRun_GateNegativeCarriesErrors(t *testing.T) {
	diags := []toolchain.Diagnostic{{Severity: toolchain.SeverityError, Message: "expected `;`", File: "src/main.rs", Line: 3}}
	r, err := FromRun([]string{"language-tools", "build"}, run(clikit.StatusGateNegative, toolchain.Counts{Errors: 1}, diags, 0))
	if err != nil {
		t.Fatalf("FromRun: %v", err)
	}
	if r.Status != clikit.StatusGateNegative {
		t.Fatalf("status = %q, want gate_negative", r.Status)
	}
	if r.ExitCode != clikit.StatusGateNegative.ExitCode() {
		t.Errorf("exit code = %d, want %d", r.ExitCode, clikit.StatusGateNegative.ExitCode())
	}
	if len(r.Errors) != 1 {
		t.Fatalf("errors = %d, want 1", len(r.Errors))
	}
}

// toolchain.Run documents that a non-zero exit always produces at least a
// synthetic diagnostic, but this layer must not trust that blindly: a
// gate_negative RunResult with zero parsed diagnostics is a real path this
// package has to cover with its own defensive floor, not silently emit a
// clikit.Result with an empty Errors list.
func TestFromRun_GateNegativeWithNoDiagnosticsGetsSyntheticError(t *testing.T) {
	r, err := FromRun([]string{"language-tools", "build"}, run(clikit.StatusGateNegative, toolchain.Counts{Errors: 1}, nil, 0))
	if err != nil {
		t.Fatalf("FromRun: %v", err)
	}
	if len(r.Errors) != 1 {
		t.Fatalf("errors = %d, want 1 synthetic error", len(r.Errors))
	}
	if r.Errors[0].Code != "gate_negative.toolchain.unparsed_failure" {
		t.Errorf("synthetic error code = %q, want gate_negative.toolchain.unparsed_failure", r.Errors[0].Code)
	}
}

// A run whose diagnostics exceeded toolchain's cap carries the first
// MaxDiagnostics verbatim plus a non-zero Overflow count; FromRun must
// render both the capped diagnostics and one extra note about the
// overflow, and must not also apply its empty-diagnostics defensive floor
// since the diagnostic list here is not actually empty.
func TestFromRun_OverflowAddsExtraDiagnostic(t *testing.T) {
	capped := make([]toolchain.Diagnostic, 20)
	for i := range capped {
		capped[i] = toolchain.Diagnostic{Severity: toolchain.SeverityError, Message: "error"}
	}
	r, err := FromRun([]string{"language-tools", "lint"}, run(clikit.StatusGateNegative, toolchain.Counts{Errors: 25}, capped, 5))
	if err != nil {
		t.Fatalf("FromRun: %v", err)
	}
	// 20 capped diagnostics plus one overflow note.
	if len(r.Errors) != 21 {
		t.Fatalf("errors = %d, want 21 (20 capped + 1 overflow note)", len(r.Errors))
	}
	found := false
	for _, e := range r.Errors {
		if e.Code == "gate_negative.toolchain.overflow" {
			found = true
		}
	}
	if !found {
		t.Errorf("no overflow diagnostic among errors: %+v", r.Errors)
	}
}

func TestFromRun_UnexpectedStatusIsAnError(t *testing.T) {
	_, err := FromRun([]string{"language-tools", "build"}, run(clikit.StatusUnsupported, toolchain.Counts{}, nil, 0))
	if err == nil {
		t.Fatal("FromRun did not reject a toolchain status outside its binary contract (success/gate_negative)")
	}
}

func TestFromRun_DataCarriesRunIdentity(t *testing.T) {
	r, err := FromRun([]string{"language-tools", "build"}, run(clikit.StatusSuccess, toolchain.Counts{}, nil, 0))
	if err != nil {
		t.Fatalf("FromRun: %v", err)
	}
	for _, key := range []string{"tool", "language", "impact", "errors", "warnings", "overflow", "duration_ms", "log_ref"} {
		if _, ok := r.Data[key]; !ok {
			t.Errorf("data missing key %q: %+v", key, r.Data)
		}
	}
}
