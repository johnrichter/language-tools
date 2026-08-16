package release

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

func TestBuild_RequiresEveryOption(t *testing.T) {
	base := Options{
		ModuleDir:  ".",
		OutputDir:  "dist",
		BinaryName: "x",
		Version:    "v1",
		Targets:    []Target{{OS: "linux", Arch: "amd64"}},
		Timeout:    time.Minute,
	}
	cases := []struct {
		name   string
		mutate func(*Options)
	}{
		{"empty ModuleDir", func(o *Options) { o.ModuleDir = "" }},
		{"empty OutputDir", func(o *Options) { o.OutputDir = "" }},
		{"empty BinaryName", func(o *Options) { o.BinaryName = "" }},
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
		ModuleDir:  moduleDir,
		OutputDir:  outputDir,
		BinaryName: "fixture",
		Version:    "v0.0.1",
		Targets:    []Target{{OS: "linux", Arch: "amd64"}, {OS: "darwin", Arch: "arm64"}},
		Timeout:    2 * time.Minute,
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
		ModuleDir:  moduleDir,
		OutputDir:  outputDir,
		BinaryName: "fixture",
		Version:    "v0.0.1",
		Targets:    []Target{{OS: "linux", Arch: "amd64"}, {OS: "bogus-os", Arch: "bogus-arch"}},
		Timeout:    2 * time.Minute,
	})
	if err == nil {
		t.Fatal("Build with an unbuildable target: want error, got none")
	}
	entries, _ := os.ReadDir(outputDir)
	for _, e := range entries {
		if e.Name() == "checksums.txt" {
			t.Error("checksums.txt was written despite the run aborting on a bad target")
		}
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
