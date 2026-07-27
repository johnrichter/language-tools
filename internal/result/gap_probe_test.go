package result

import (
	"strings"
	"testing"

	"github.com/johnrichter/claude-shared-tooling/go/clikit"
	"github.com/johnrichter/claude-shared-tooling/go/toolchain"
)

func TestProbe_LongMessage(t *testing.T) {
	long := strings.Repeat("mismatched types\n", 400) // ~6800 bytes, collapses to one long line
	diags := []toolchain.Diagnostic{{Severity: toolchain.SeverityError, Message: long}}
	_, err := FromRun([]string{"language-tools", "build"}, run(clikit.StatusGateNegative, toolchain.Counts{Errors: 1}, diags, 0))
	if err != nil {
		t.Fatalf("PROBE long-message FAILED FromRun: %v", err)
	}
}

func TestProbe_EmptyAfterSanitize(t *testing.T) {
	diags := []toolchain.Diagnostic{{Severity: toolchain.SeverityError, Message: "\n\t\r "}} // all control/space, File empty
	_, err := FromRun([]string{"language-tools", "build"}, run(clikit.StatusGateNegative, toolchain.Counts{Errors: 1}, diags, 0))
	if err != nil {
		t.Fatalf("PROBE empty-message FAILED FromRun: %v", err)
	}
}
