package release

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseTarget(t *testing.T) {
	cases := []struct {
		in      string
		wantOS  string
		wantArc string
		wantErr bool
	}{
		{"linux/amd64", "linux", "amd64", false},
		{"darwin/arm64", "darwin", "arm64", false},
		{"noSlash", "", "", true},
		{"/amd64", "", "", true},
		{"linux/", "", "", true},
		{"", "", "", true},
	}
	for _, c := range cases {
		got, err := ParseTarget(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseTarget(%q): want error, got %+v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseTarget(%q): unexpected error %v", c.in, err)
			continue
		}
		if got.OS != c.wantOS || got.Arch != c.wantArc {
			t.Errorf("ParseTarget(%q) = %+v, want {%s %s}", c.in, got, c.wantOS, c.wantArc)
		}
	}
}

func TestParseBinarySpec(t *testing.T) {
	cases := []struct {
		in       string
		wantName string
		wantPkg  string
		wantErr  bool
	}{
		{"language-tools:.", "language-tools", ".", false},
		{"foo:./cmd/foo", "foo", "./cmd/foo", false},
		{"noColon", "", "", true},
		{":./cmd/foo", "", "", true},
		{"foo:", "", "", true},
		{"", "", "", true},
	}
	for _, c := range cases {
		got, err := ParseBinarySpec(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseBinarySpec(%q): want error, got %+v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseBinarySpec(%q): unexpected error %v", c.in, err)
			continue
		}
		if got.Name != c.wantName || got.Package != c.wantPkg {
			t.Errorf("ParseBinarySpec(%q) = %+v, want {%s %s}", c.in, got, c.wantName, c.wantPkg)
		}
	}
}

func TestBuild_RequiresEveryOption(t *testing.T) {
	base := Options{
		ModuleDir: ".",
		OutputDir: "dist",
		Binaries:  []Binary{{Name: "x", Package: "."}},
		Version:   "v1",
		Targets:   []Target{{OS: "linux", Arch: "amd64"}},
		Timeout:   time.Minute,
	}
	cases := []struct {
		name   string
		mutate func(*Options)
	}{
		{"empty ModuleDir", func(o *Options) { o.ModuleDir = "" }},
		{"empty OutputDir", func(o *Options) { o.OutputDir = "" }},
		{"empty Binaries", func(o *Options) { o.Binaries = nil }},
		{"empty Version", func(o *Options) { o.Version = "" }},
		{"empty Targets", func(o *Options) { o.Targets = nil }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := base
			c.mutate(&opts)
			if _, err := Build(context.Background(), opts); err == nil {
				t.Errorf("Build with %s: want error, got none", c.name)
			}
		})
	}
}

// fixtureModule writes a trivial buildable Go module to a temp dir so this
// test never depends on language-tools' own module (which requires the
// sibling ai-shared-lib replace paths this package's own test run may not
// have) and exercises the real cross-compile + archive + checksum path.
func fixtureModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	main := `package main
func main() { println("fixture") }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestBuild_EndToEnd(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	moduleDir := fixtureModule(t)
	outputDir := t.TempDir()

	artifacts, err := Build(context.Background(), Options{
		ModuleDir: moduleDir,
		OutputDir: outputDir,
		Binaries:  []Binary{{Name: "fixture", Package: "."}},
		Version:   "0.0.1",
		Targets:   []Target{{OS: "linux", Arch: "amd64"}, {OS: "darwin", Arch: "arm64"}},
		Timeout:   2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("artifacts = %d, want 2", len(artifacts))
	}

	for _, a := range artifacts {
		data, err := os.ReadFile(a.ArchivePath)
		if err != nil {
			t.Fatalf("read archive %s: %v", a.ArchivePath, err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != a.ChecksumSHA256 {
			t.Errorf("%s: checksum mismatch, artifact says %s, computed %s", a.ArchivePath, a.ChecksumSHA256, hex.EncodeToString(sum[:]))
		}
		verifyTarGzContainsBinary(t, a.ArchivePath, "fixture")
	}

	checksumsPath := filepath.Join(outputDir, "checksums.txt")
	raw, err := os.ReadFile(checksumsPath)
	if err != nil {
		t.Fatalf("read checksums.txt: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != len(artifacts) {
		t.Fatalf("checksums.txt has %d lines, want %d", len(lines), len(artifacts))
	}
	for _, a := range artifacts {
		base := filepath.Base(a.ArchivePath)
		wantLine := a.ChecksumSHA256 + "  " + base
		found := false
		for _, l := range lines {
			if l == wantLine {
				found = true
			}
		}
		if !found {
			t.Errorf("checksums.txt missing expected line %q; got %v", wantLine, lines)
		}
	}

	// binary-checksums.txt golden row format: "<sha256>  <name>_<version>_<os>_<arch>",
	// no file extension, matching the fleet's shared provisioner key.
	binChecksumsPath := filepath.Join(outputDir, "binary-checksums.txt")
	binRaw, err := os.ReadFile(binChecksumsPath)
	if err != nil {
		t.Fatalf("read binary-checksums.txt: %v", err)
	}
	binLines := strings.Split(strings.TrimRight(string(binRaw), "\n"), "\n")
	if len(binLines) != len(artifacts) {
		t.Fatalf("binary-checksums.txt has %d lines, want %d", len(binLines), len(artifacts))
	}
	for _, a := range artifacts {
		wantLine := fmt.Sprintf("%s  fixture_0.0.1_%s_%s", a.BinarySHA256, a.Target.OS, a.Target.Arch)
		found := false
		for _, l := range binLines {
			if l == wantLine {
				found = true
			}
		}
		if !found {
			t.Errorf("binary-checksums.txt missing expected line %q; got %v", wantLine, binLines)
		}
	}
}

// TestBuild_MultipleBinaries pins the repeatable --binary contract at the
// library level: one run of two binaries across two targets produces four
// archives, one checksums.txt row per archive, and one binary-checksums.txt
// row per extracted binary — each keyed on its own name, not the other's.
func TestBuild_MultipleBinaries(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	moduleDir := fixtureModule(t)
	outputDir := t.TempDir()

	artifacts, err := Build(context.Background(), Options{
		ModuleDir: moduleDir,
		OutputDir: outputDir,
		Binaries:  []Binary{{Name: "alpha", Package: "."}, {Name: "beta", Package: "."}},
		Version:   "0.0.1",
		Targets:   []Target{{OS: "linux", Arch: "amd64"}},
		Timeout:   2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("artifacts = %d, want 2", len(artifacts))
	}

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	archiveCount := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tar.gz") {
			archiveCount++
		}
	}
	if archiveCount != 2 {
		t.Errorf("archive count = %d, want 2", archiveCount)
	}

	binRaw, err := os.ReadFile(filepath.Join(outputDir, "binary-checksums.txt"))
	if err != nil {
		t.Fatalf("read binary-checksums.txt: %v", err)
	}
	for _, wantKey := range []string{"alpha_0.0.1_linux_amd64", "beta_0.0.1_linux_amd64"} {
		if !strings.Contains(string(binRaw), wantKey) {
			t.Errorf("binary-checksums.txt missing key %q:\n%s", wantKey, binRaw)
		}
	}
}

// A build target no Go toolchain recognizes must fail the whole run, not
// silently skip that one target and still report success for the rest —
// Build's own doc says a failure on any target aborts the whole run.
func TestBuild_UnknownTargetAbortsWholeRun(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	moduleDir := fixtureModule(t)
	outputDir := t.TempDir()

	_, err := Build(context.Background(), Options{
		ModuleDir: moduleDir,
		OutputDir: outputDir,
		Binaries:  []Binary{{Name: "fixture", Package: "."}},
		Version:   "0.0.1",
		Targets:   []Target{{OS: "linux", Arch: "amd64"}, {OS: "bogus-os", Arch: "bogus-arch"}},
		Timeout:   2 * time.Minute,
	})
	if err == nil {
		t.Fatal("Build with an unbuildable target: want error, got none")
	}
	entries, _ := os.ReadDir(outputDir)
	for _, e := range entries {
		if e.Name() == "checksums.txt" {
			t.Error("checksums.txt was written despite the run aborting on a bad target")
		}
		if e.Name() == "binary-checksums.txt" {
			t.Error("binary-checksums.txt was written despite the run aborting on a bad target")
		}
	}
}

// TestBuild_ArgvNamesBuildVCS pins the `go build` argv itself: it must name
// -buildvcs=true explicitly rather than relying on Go's inconsistent
// default (embed VCS info in a real clone, silently omit it elsewhere).
func TestBuild_ArgvNamesBuildVCS(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	moduleDir := fixtureModule(t)
	// go build only embeds VCS info for a module dir inside a repo with a
	// commit; a bare temp dir has neither.
	for _, args := range [][]string{
		{"init"},
		{"-c", "user.email=test@test", "-c", "user.name=test", "add", "."},
		{"-c", "user.email=test@test", "-c", "user.name=test", "commit", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = moduleDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	outputDir := t.TempDir()

	if _, err := Build(context.Background(), Options{
		ModuleDir: moduleDir,
		OutputDir: outputDir,
		Binaries:  []Binary{{Name: "fixture", Package: "."}},
		Version:   "0.0.1",
		Targets:   []Target{{OS: "linux", Arch: "amd64"}},
		Timeout:   2 * time.Minute,
	}); err != nil {
		t.Fatalf("Build: %v", err)
	}

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	var archivePath string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tar.gz") {
			archivePath = filepath.Join(outputDir, e.Name())
		}
	}
	if archivePath == "" {
		t.Fatal("no archive produced")
	}

	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	if _, err := tr.Next(); err != nil {
		t.Fatal(err)
	}
	binBytes, err := io.ReadAll(tr)
	if err != nil {
		t.Fatal(err)
	}
	tmpBin := filepath.Join(t.TempDir(), "fixture")
	if err := os.WriteFile(tmpBin, binBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("go", "version", "-m", tmpBin).CombinedOutput()
	if err != nil {
		t.Fatalf("go version -m: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "vcs.revision") {
		t.Errorf("built binary has no vcs.revision build info; -buildvcs=true was not honored:\n%s", out)
	}
}

// TestBuild_FailsWithoutGitOnPATH exercises the argv change's whole point:
// with -buildvcs=true and git absent, `go build` must hard-fail rather than
// silently ship a binary with no VCS stamp.
func TestBuild_FailsWithoutGitOnPATH(t *testing.T) {
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	// A real clone: `go build -buildvcs=true` only attempts VCS detection
	// (and thus can fail without git) when ModuleDir is inside a repo.
	if _, err := exec.Command("git", "-C", "../..", "rev-parse", "--is-inside-work-tree").Output(); err != nil {
		t.Skip("test requires running inside a git clone")
	}

	moduleDir, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	outputDir := t.TempDir()

	// A PATH containing only the directory holding the real `go` binary,
	// with no `git` anywhere on it.
	pathDir := filepath.Dir(goPath)
	t.Setenv("PATH", pathDir)

	_, err = Build(context.Background(), Options{
		ModuleDir: moduleDir,
		OutputDir: outputDir,
		Binaries:  []Binary{{Name: "language-tools", Package: "."}},
		Version:   "0.0.1",
		Targets:   []Target{{OS: "linux", Arch: "amd64"}},
		Timeout:   2 * time.Minute,
	})
	if err == nil {
		t.Fatal("Build with git absent from PATH: want error, got none")
	}
	if !strings.Contains(err.Error(), "error obtaining VCS status") {
		t.Errorf("Build error = %v, want it to contain %q", err, "error obtaining VCS status")
	}
}

func verifyTarGzContainsBinary(t *testing.T, archivePath, wantName string) {
	t.Helper()
	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open %s: %v", archivePath, err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader for %s: %v", archivePath, err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("tar entry for %s: %v", archivePath, err)
	}
	if hdr.Name != wantName {
		t.Errorf("%s: tar entry name = %q, want %q", archivePath, hdr.Name, wantName)
	}
	if _, err := io.Copy(io.Discard, tr); err != nil {
		t.Errorf("%s: read tar entry body: %v", archivePath, err)
	}
	if _, err := tr.Next(); err != io.EOF {
		t.Errorf("%s: archive has more than one entry", archivePath)
	}
}
