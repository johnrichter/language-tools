package pin

import (
	"strconv"
	"strings"
)

// satisfies reports whether version meets pin: equal to it or newer,
// component by component over each dotted-decimal segment — never a
// string match. "1.26.5" satisfies a "1.24.0" pin despite the two strings
// sharing no equality or substring relationship; an exact-match rule would
// wrongly reject it, and would break on the next patch release of any
// pinned toolchain.
func satisfies(version, pin string) bool {
	return compareVersions(version, pin) >= 0
}

// compareVersions returns -1, 0, or 1 as a sits below, equal to, or above
// b, comparing each dotted-decimal segment as an integer. A side with fewer
// segments is padded with zeros, so "1.82" and "1.82.0" compare equal. A
// non-numeric segment reads as zero rather than panicking or erroring —
// this package only ever calls it with version strings it has already
// pulled from a versioned source (a pin, a declared floor, or a resolved
// toolchain), so a malformed segment reflects a foreign input, not a bug
// here to surface loudly.
func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(as) {
			av, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bv, _ = strconv.Atoi(bs[i])
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}
