// Package result translates a toolchain.RunResult into the clikit.Result
// every language-tools subcommand emits. It adds no verdict of its own —
// toolchain.Run already decided pass/fail — it only carries that verdict,
// and the diagnostics behind it, onto the clikit wire shape.
package result

import (
	"fmt"
	"strings"

	"github.com/johnrichter/claude-shared-tooling/go/clikit"
	"github.com/johnrichter/claude-shared-tooling/go/toolchain"
)

// FromRun builds the clikit.Result for one build/test/lint invocation.
//
// toolchain.RunResult.Status is binary (success or gate_negative); a
// success that only passed because the run allowed warnings still carries
// those warnings in Counts, so this layer promotes that case to
// clikit.StatusCaveats — a qualified success, which is what happened —
// without re-deriving pass/fail itself.
func FromRun(command []string, run *toolchain.RunResult) (*clikit.Result, error) {
	data := runData(run)

	switch run.Status {
	case clikit.StatusSuccess:
		if run.Counts.Warnings == 0 {
			return clikit.NewSuccess(command, data)
		}
		caveats, err := diagnostics(run, clikit.StatusCaveats)
		if err != nil {
			return nil, err
		}
		return clikit.NewCaveats(command, data, caveats)

	case clikit.StatusGateNegative:
		errs, err := diagnostics(run, clikit.StatusGateNegative)
		if err != nil {
			return nil, err
		}
		if len(errs) == 0 {
			// toolchain.Run guarantees a synthetic Diagnostic whenever a
			// non-zero exit produced no parsed error, so this is a defensive
			// floor, not an expected path.
			d, err := clikit.NewError(
				"gate_negative.toolchain.unparsed_failure",
				fmt.Sprintf("%s reported failure with no parsed diagnostic; see %s", run.Tool, run.LogRef),
				clikit.Manual(fmt.Sprintf("inspect %s for the tool's raw output", run.LogRef)),
				nil,
			)
			if err != nil {
				return nil, err
			}
			errs = []clikit.Diagnostic{d}
		}
		return clikit.NewGateNegative(command, data, errs, nil)

	default:
		return nil, fmt.Errorf("result: toolchain reported unexpected status %q", run.Status)
	}
}

// runData is the bounded summary every FromRun result carries in `data`:
// the run's identity and totals, never the diagnostic detail (that belongs
// in errors/caveats, capped the same way toolchain already capped it).
func runData(run *toolchain.RunResult) map[string]any {
	return map[string]any{
		"tool":        run.Tool,
		"language":    run.Language,
		"impact":      string(run.Impact),
		"errors":      run.Counts.Errors,
		"warnings":    run.Counts.Warnings,
		"overflow":    run.Overflow,
		"duration_ms": run.DurationMS,
		"log_ref":     run.LogRef,
	}
}

// diagnostics renders run.Diagnostics as clikit diagnostics of class, plus
// one extra entry noting an overflow past toolchain's cap, if any.
func diagnostics(run *toolchain.RunResult, class clikit.Status) ([]clikit.Diagnostic, error) {
	out := make([]clikit.Diagnostic, 0, len(run.Diagnostics)+1)
	for _, d := range run.Diagnostics {
		diag, err := diagnostic(class, d, run.LogRef)
		if err != nil {
			return nil, err
		}
		out = append(out, diag)
	}
	if run.Overflow > 0 {
		msg := fmt.Sprintf("%d more diagnostics were recorded past the capped list; see %s for all of them", run.Overflow, run.LogRef)
		code := string(class) + ".toolchain.overflow"
		triage := clikit.Manual(fmt.Sprintf("review %s for the diagnostics beyond the cap", run.LogRef))
		var (
			diag clikit.Diagnostic
			err  error
		)
		if class == clikit.StatusCaveats {
			diag, err = clikit.NewCaveat(code, msg, triage, nil)
		} else {
			diag, err = clikit.NewError(code, msg, triage, nil)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, diag)
	}
	return out, nil
}

// diagnostic renders one toolchain.Diagnostic as a clikit diagnostic of
// class, carrying its file/line/tool-code as context and pointing at
// logRef for the tool's full output.
func diagnostic(class clikit.Status, d toolchain.Diagnostic, logRef string) (clikit.Diagnostic, error) {
	code := string(class) + ".toolchain." + string(d.Severity)
	msg := OneLine(d.Message)
	if d.File != "" {
		if d.Line > 0 {
			msg = fmt.Sprintf("%s:%d: %s", d.File, d.Line, msg)
		} else {
			msg = fmt.Sprintf("%s: %s", d.File, msg)
		}
	}
	context := map[string]any{}
	if d.Code != "" {
		context["tool_code"] = d.Code
	}
	if d.File != "" {
		context["file"] = d.File
	}
	if d.Line > 0 {
		context["line"] = d.Line
	}
	if len(context) == 0 {
		context = nil
	}
	triage := clikit.Manual(fmt.Sprintf("fix the reported diagnostic; see %s for the tool's full output", logRef))
	if class == clikit.StatusCaveats {
		return clikit.NewCaveat(code, msg, triage, context)
	}
	return clikit.NewError(code, msg, triage, context)
}

// OneLine collapses text onto a single clikit-safe line: any run of control
// characters (the embedded newlines/tabs a tool's raw diagnostic or a failed
// subprocess error carries) becomes one space, the result is bounded under
// clikit's line schema (non-empty, at most 4096 chars), and truncation never
// leaves a partial UTF-8 rune. clikit rejects a message that is empty, too
// long, or holds a control character, so without this the whole diagnostic
// fails to construct and a genuine gate_negative failure is misreported as an
// internal fault one layer up. Exported so cmd's own clikit-message paths
// share the one implementation rather than re-deriving it (cmd already
// imports this package, so the reverse import would cycle).
func OneLine(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	out := strings.TrimSpace(b.String())
	const maxLen = 4000
	if len(out) > maxLen {
		out = strings.ToValidUTF8(out[:maxLen], "")
	}
	if out == "" {
		return "(no detail)"
	}
	return out
}
