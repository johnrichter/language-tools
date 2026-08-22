package lint

import (
	"github.com/gordonklaus/ineffassign/pkg/ineffassign"
	"github.com/kisielk/errcheck/errcheck"
	"golang.org/x/tools/go/analysis"
	honnef "honnef.co/go/tools/analysis/lint"
	"honnef.co/go/tools/simple"
	"honnef.co/go/tools/staticcheck"
	"honnef.co/go/tools/stylecheck"
	"honnef.co/go/tools/unused"
)

// unusedAnalyzer is U1000, the one member of the set that reports nothing
// through its pass: it returns an unused.Result the driver reads instead
// (see readUnused).
var unusedAnalyzer = unused.Analyzer.Analyzer

// Analyzers returns the analyzer set the driver runs, in a stable order:
// unchecked errors (errcheck), ineffectual assignments (ineffassign), and
// the SA/S/ST/U1000 families staticcheck ships as libraries.
//
// The set deliberately excludes go/analysis's own vet suite — `go vet` is
// that same suite, and the toolchain adapter runs it natively as its own
// check rather than folding it into lint. Every analyzer here is registered
// unconditionally, including the ones staticcheck's own CLI leaves off by
// default (e.g. ST1000, ST1003): a driver has no configuration surface yet,
// so the honest set is the whole set.
func Analyzers() []*analysis.Analyzer {
	set := []*analysis.Analyzer{errcheck.Analyzer, ineffassign.Analyzer}
	for _, family := range [][]*honnef.Analyzer{
		staticcheck.Analyzers,
		simple.Analyzers,
		stylecheck.Analyzers,
		{unused.Analyzer},
	} {
		for _, a := range family {
			set = append(set, a.Analyzer)
		}
	}
	return set
}
