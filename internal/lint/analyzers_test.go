package lint

import (
	"regexp"
	"testing"
)

// familyCode matches the name of every analyzer the set may hold: the two
// standalone tools, or one of the four staticcheck families. `go vet`'s own
// passes are named as lowercase words (printf, copylocks, ...), so this
// pattern is also what pins them out of the set — vet is its own check, run
// natively, and folding its suite into lint would double-report it.
var familyCode = regexp.MustCompile(`^(errcheck|ineffassign|SA\d+|S\d+|ST\d+|U\d+)$`)

func TestAnalyzers_HoldsEveryFamilyAndNothingElse(t *testing.T) {
	families := map[string]*regexp.Regexp{
		"errcheck":    regexp.MustCompile(`^errcheck$`),
		"ineffassign": regexp.MustCompile(`^ineffassign$`),
		"staticcheck": regexp.MustCompile(`^SA\d+$`),
		"simple":      regexp.MustCompile(`^S\d+$`),
		"stylecheck":  regexp.MustCompile(`^ST\d+$`),
		"unused":      regexp.MustCompile(`^U\d+$`),
	}
	seen := map[string]bool{}
	names := map[string]bool{}
	for _, a := range Analyzers() {
		if !familyCode.MatchString(a.Name) {
			t.Errorf("analyzer %q belongs to none of the registered families", a.Name)
		}
		if names[a.Name] {
			t.Errorf("analyzer %q is registered twice; a driver rejects a duplicate name", a.Name)
		}
		names[a.Name] = true
		for family, pattern := range families {
			if pattern.MatchString(a.Name) {
				seen[family] = true
			}
		}
	}
	for family := range families {
		if !seen[family] {
			t.Errorf("no analyzer from the %s family is registered", family)
		}
	}
}

func TestAnalyzers_ReadsUnusedThroughItsResult(t *testing.T) {
	if unusedAnalyzer.ResultType == nil {
		t.Fatal("U1000 declares no result type; the driver reads its findings from that result, not from its pass")
	}
	for _, a := range Analyzers() {
		if a == unusedAnalyzer {
			return
		}
	}
	t.Error("U1000 is not in the registered set")
}
