// Package release cross-compiles this module into per-OS/arch binaries and
// packages each as a checksummed archive — the fleet's release-build
// orchestration entrypoint (SC-DISTRIBUTION: CLIs ship binaries +
// checksums, never committed to the repo).
package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/johnrichter/claude-shared-tooling/go/fsx"
	"github.com/johnrichter/claude-shared-tooling/go/sysops"
)

// Target is one GOOS/GOARCH pair to build for.
type Target struct {
	OS   string
	Arch string
}

// String renders t as Go's own "os/arch" pairing.
func (t Target) String() string { return t.OS + "/" + t.Arch }

// ParseTarget parses "os/arch" (e.g. "linux/amd64") into a Target.
func ParseTarget(s string) (Target, error) {
	osName, arch, ok := strings.Cut(s, "/")
	if !ok || osName == "" || arch == "" {
		return Target{}, fmt.Errorf("release: invalid target %q, want \"os/arch\"", s)
	}
	return Target{OS: osName, Arch: arch}, nil
}

// Binary is one named main package this build produces a binary for.
type Binary struct {
	// Name is the binary's base name, embedded in each of its archives'
	// filenames, the file inside them, and its binary-checksums.txt key.
	Name string
	// Package is the `go build` argument naming its main package, resolved
	// relative to Options.ModuleDir (e.g. "." or "./cmd/foo").
	Package string
}

// ParseBinarySpec parses "name:package" (e.g. "language-tools:.") into a
// Binary.
func ParseBinarySpec(s string) (Binary, error) {
	name, pkg, ok := strings.Cut(s, ":")
	if !ok || name == "" || pkg == "" {
		return Binary{}, fmt.Errorf("release: invalid binary %q, want \"name:package\"", s)
	}
	return Binary{Name: name, Package: pkg}, nil
}

// Options configures Build.
type Options struct {
	// ModuleDir is the Go module root every Binary's Package is resolved
	// against.
	ModuleDir string
	// OutputDir is where archives and the checksum manifests are written.
	OutputDir string
	// Binaries is the set of named main packages to build. Empty is
	// rejected — a caller names its binaries explicitly rather than relying
	// on an implicit default here.
	Binaries []Binary
	// Version tags each archive's filename and binary-checksums.txt key,
	// e.g. "1.2.3" — no leading "v"; the fleet's shared provisioner
	// convention adds that itself where a "v"-prefixed tag is needed.
	Version string
	// Targets is the build matrix. Empty is rejected — a caller names its
	// targets explicitly rather than relying on an implicit default here.
	Targets []Target
	// Timeout bounds each per-binary-per-target `go build` invocation.
	Timeout time.Duration
}

// Artifact is one binary/target pair's packaged build output.
type Artifact struct {
	Binary         Binary
	Target         Target
	ArchivePath    string
	ChecksumSHA256 string
	// BinarySHA256 is the digest of the extracted binary itself, distinct
	// from ChecksumSHA256 (the archive's digest).
	BinarySHA256 string
}

// Build cross-compiles every opts.Binaries entry for every target in
// opts.Targets, archives each as a .tar.gz, and writes checksums.txt (one
// row per archive) and binary-checksums.txt (one row per extracted binary)
// alongside them. It returns the artifacts in binary-then-target order; a
// failure on any of them aborts the whole run rather than shipping a
// partial release.
func Build(ctx context.Context, opts Options) ([]Artifact, error) {
	if opts.ModuleDir == "" {
		return nil, fmt.Errorf("release: options.ModuleDir is required")
	}
	if opts.OutputDir == "" {
		return nil, fmt.Errorf("release: options.OutputDir is required")
	}
	if len(opts.Binaries) == 0 {
		return nil, fmt.Errorf("release: options.Binaries is required")
	}
	if opts.Version == "" {
		return nil, fmt.Errorf("release: options.Version is required")
	}
	if len(opts.Targets) == 0 {
		return nil, fmt.Errorf("release: options.Targets is required")
	}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return nil, fmt.Errorf("release: create output dir %s: %w", opts.OutputDir, err)
	}

	artifacts := make([]Artifact, 0, len(opts.Binaries)*len(opts.Targets))
	for _, binary := range opts.Binaries {
		for _, target := range opts.Targets {
			artifact, err := buildOne(ctx, opts, binary, target)
			if err != nil {
				return nil, fmt.Errorf("release: build %s for %s: %w", binary.Name, target, err)
			}
			artifacts = append(artifacts, artifact)
		}
	}
	if err := writeChecksums(opts.OutputDir, artifacts); err != nil {
		return nil, err
	}
	if err := writeBinaryChecksums(opts.OutputDir, opts.Version, artifacts); err != nil {
		return nil, err
	}
	return artifacts, nil
}

// buildOne compiles binary for target into a temporary binary, archives it,
// and writes the archive plus its checksum.
func buildOne(ctx context.Context, opts Options, binary Binary, target Target) (Artifact, error) {
	workDir, err := os.MkdirTemp("", "language-tools-release-*")
	if err != nil {
		return Artifact{}, fmt.Errorf("stage build dir: %w", err)
	}
	defer func() {
		if rmErr := os.RemoveAll(workDir); rmErr != nil {
			fmt.Fprintf(os.Stderr, "release: clean up staging dir %s: %v\n", workDir, rmErr)
		}
	}()

	binName := binary.Name
	if target.OS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(workDir, binName)

	env := append(os.Environ(), "GOOS="+target.OS, "GOARCH="+target.Arch, "CGO_ENABLED=0")
	argv := []string{"build", "-trimpath", "-ldflags=-s -w", "-o", binPath, binary.Package}
	execRes, err := sysops.Run(ctx, "go", argv, sysops.Options{Dir: opts.ModuleDir, Env: env, Timeout: opts.Timeout})
	if err != nil {
		return Artifact{}, fmt.Errorf("run go build: %w", err)
	}
	if execRes.ExitCode != 0 {
		return Artifact{}, fmt.Errorf("go build exited %d: %s", execRes.ExitCode, execRes.Stderr)
	}

	binBytes, err := os.ReadFile(binPath)
	if err != nil {
		return Artifact{}, fmt.Errorf("read built binary: %w", err)
	}

	archiveName := fmt.Sprintf("%s_%s_%s_%s.tar.gz", binary.Name, opts.Version, target.OS, target.Arch)
	archiveBytes, err := archiveTarGz(binName, binBytes)
	if err != nil {
		return Artifact{}, err
	}
	archivePath := filepath.Join(opts.OutputDir, archiveName)
	if err := fsx.WriteAtomic(archivePath, archiveBytes, 0o644); err != nil {
		return Artifact{}, fmt.Errorf("write archive %s: %w", archivePath, err)
	}

	archiveSum := sha256.Sum256(archiveBytes)
	binSum := sha256.Sum256(binBytes)
	return Artifact{
		Binary:         binary,
		Target:         target,
		ArchivePath:    archivePath,
		ChecksumSHA256: hex.EncodeToString(archiveSum[:]),
		BinarySHA256:   hex.EncodeToString(binSum[:]),
	}, nil
}

// archiveTarGz packages a single file as a gzipped tar archive, the shape
// every artifact ships in regardless of target OS.
func archiveTarGz(name string, data []byte) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	hdr := &tar.Header{
		Name: name,
		Mode: int64(fs.FileMode(0o755)),
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return nil, fmt.Errorf("write tar header: %w", err)
	}
	if _, err := tw.Write(data); err != nil {
		return nil, fmt.Errorf("write tar body: %w", err)
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("close tar writer: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("close gzip writer: %w", err)
	}
	return buf.Bytes(), nil
}

// writeChecksums writes checksums.txt: one "<sha256>  <archive-basename>"
// line per artifact, sorted by filename — the shape a download-script's
// `sha256sum -c` expects.
func writeChecksums(outputDir string, artifacts []Artifact) error {
	lines := make([]string, len(artifacts))
	for i, a := range artifacts {
		lines[i] = fmt.Sprintf("%s  %s\n", a.ChecksumSHA256, filepath.Base(a.ArchivePath))
	}
	sort.Strings(lines)
	path := filepath.Join(outputDir, "checksums.txt")
	if err := fsx.WriteAtomic(path, []byte(strings.Join(lines, "")), 0o644); err != nil {
		return fmt.Errorf("release: write checksums manifest: %w", err)
	}
	return nil
}

// binaryChecksumKey renders the fleet's shared provisioner key: four
// underscore-joined tokens (name, version, os, arch) with no file
// extension — the same key a provisioner derives from an archive filename
// after stripping ".tar.gz".
func binaryChecksumKey(name, version string, target Target) string {
	return fmt.Sprintf("%s_%s_%s_%s", name, version, target.OS, target.Arch)
}

// writeBinaryChecksums writes binary-checksums.txt: one "<sha256>  <key>"
// line per artifact, keyed on the extracted binary rather than its archive
// — the digest a provisioner checks after unpacking, sorted by key.
func writeBinaryChecksums(outputDir, version string, artifacts []Artifact) error {
	lines := make([]string, len(artifacts))
	for i, a := range artifacts {
		lines[i] = fmt.Sprintf("%s  %s\n", a.BinarySHA256, binaryChecksumKey(a.Binary.Name, version, a.Target))
	}
	sort.Strings(lines)
	path := filepath.Join(outputDir, "binary-checksums.txt")
	if err := fsx.WriteAtomic(path, []byte(strings.Join(lines, "")), 0o644); err != nil {
		return fmt.Errorf("release: write binary-checksums manifest: %w", err)
	}
	return nil
}
