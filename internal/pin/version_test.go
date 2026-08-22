package pin

import (
	"context"
	"testing"

	"github.com/johnrichter/claude-shared-tooling/go/toolchain"
)

func TestRouteFor(t *testing.T) {
	if got := routeFor("go", toolchain.CheckLint); got != toolchain.RouteInProcess {
		t.Errorf("routeFor(go, lint) = %s, want %s", got, toolchain.RouteInProcess)
	}
	cases := []struct {
		language string
		check    toolchain.Check
	}{
		{"go", toolchain.CheckBuild},
		{"go", toolchain.CheckTest},
		{"go", toolchain.CheckFormat},
		{"go", toolchain.CheckVet},
		{"rust", toolchain.CheckLint},
		{"python", toolchain.CheckLint},
	}
	for _, c := range cases {
		if got := routeFor(c.language, c.check); got != toolchain.RouteSubprocess {
			t.Errorf("routeFor(%s, %s) = %s, want %s", c.language, c.check, got, toolchain.RouteSubprocess)
		}
	}
}

// TestResolveVersionSubprocess exercises the real go/rustc/python3 on PATH:
// this package resolves each toolchain's own version command rather than a
// fixed string, so the test only asserts the shape it must have (a
// two-or-three-part dotted number), never a specific release.
func TestResolveVersionSubprocess(t *testing.T) {
	for _, language := range []string{"go", "rust", "python"} {
		t.Run(language, func(t *testing.T) {
			version, err := resolveVersion(context.Background(), language, toolchain.RouteSubprocess)
			if err != nil {
				t.Fatalf("resolveVersion(%s, subprocess): %v (toolchain must be on PATH for this test)", language, err)
			}
			if compareVersions(version, "0.0.0") <= 0 {
				t.Errorf("resolveVersion(%s, subprocess) = %q, want a positive dotted version", language, version)
			}
		})
	}
}

func TestResolveVersionInProcess(t *testing.T) {
	version, err := resolveVersion(context.Background(), "go", toolchain.RouteInProcess)
	if err != nil {
		t.Fatalf("resolveVersion(go, in_process): %v", err)
	}
	if compareVersions(version, "0.0.0") <= 0 {
		t.Errorf("resolveVersion(go, in_process) = %q, want a positive dotted version", version)
	}
}
