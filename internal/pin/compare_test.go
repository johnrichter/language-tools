package pin

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.26.5", "1.26.5", 0},
		{"1.82", "1.82.0", 0},
		{"1.26.5", "1.24.0", 1},
		{"1.24.0", "1.26.5", -1},
		{"3.14.7", "3.9.0", 1},
		{"3.9.0", "3.14.7", -1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// TestSatisfiesIsNotExactMatch proves the rule the design demands: a newer
// resolved version satisfies an older pin despite the two strings never
// being equal, and a resolved version below the pin never satisfies it even
// when it shares a prefix.
func TestSatisfiesIsNotExactMatch(t *testing.T) {
	const resolved, pin = "1.26.5", "1.20.0"
	if resolved == pin {
		t.Fatalf("test fixture invalid: resolved and pin must differ")
	}
	if !satisfies(resolved, pin) {
		t.Errorf("satisfies(%q, %q) = false, want true (an exact-match rule would wrongly fail here)", resolved, pin)
	}
	if satisfies(pin, resolved) {
		t.Errorf("satisfies(%q, %q) = true, want false", pin, resolved)
	}
}

func TestSatisfiesEqual(t *testing.T) {
	if !satisfies("1.26.0", "1.26.0") {
		t.Error("satisfies(x, x) = false, want true")
	}
	if !satisfies("1.26", "1.26.0") {
		t.Error("satisfies with a missing trailing segment should compare equal")
	}
}
