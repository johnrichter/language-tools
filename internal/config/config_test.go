package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/pflag"
)

func checkFlags() *pflag.FlagSet {
	fs := pflag.NewFlagSet("build", pflag.ContinueOnError)
	fs.String("language", "", "")
	fs.String("dir", "", "")
	fs.String("log-dir", DefaultLogDir, "")
	fs.String("cache-dir", "", "")
	fs.Bool("allow-warnings", false, "")
	fs.Duration("timeout", 0, "")
	return fs
}

func TestLoadCheck_DefaultsOnly(t *testing.T) {
	cfg, err := LoadCheck(checkFlags(), "")
	if err != nil {
		t.Fatalf("LoadCheck: %v", err)
	}
	if cfg.Language != "" || cfg.Dir != "" {
		t.Errorf("expected empty language/dir at defaults, got %+v", cfg)
	}
	if cfg.LogDir != DefaultLogDir {
		t.Errorf("log dir = %q, want default %q", cfg.LogDir, DefaultLogDir)
	}
	if cfg.AllowWarnings {
		t.Error("allow_warnings must default to false (warnings fail by default)")
	}
}

func TestLoadCheck_FlagBeatsEverything(t *testing.T) {
	t.Setenv(EnvPrefix+"LANGUAGE", "cobol")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(cfgPath, []byte("language: fortran\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := checkFlags()
	if err := fs.Set("language", "rust"); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadCheck(fs, cfgPath)
	if err != nil {
		t.Fatalf("LoadCheck: %v", err)
	}
	if cfg.Language != "rust" {
		t.Errorf("language = %q, want rust (flag must win over env and file)", cfg.Language)
	}
}

func TestLoadCheck_EnvBeatsFile(t *testing.T) {
	t.Setenv(EnvPrefix+"LANGUAGE", "cobol")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(cfgPath, []byte("language: fortran\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadCheck(checkFlags(), cfgPath)
	if err != nil {
		t.Fatalf("LoadCheck: %v", err)
	}
	if cfg.Language != "cobol" {
		t.Errorf("language = %q, want cobol (env must win over file)", cfg.Language)
	}
}

func TestLoadCheck_FileBeatsDefault(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(cfgPath, []byte("language: fortran\nallow_warnings: true\ntimeout: 90s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadCheck(checkFlags(), cfgPath)
	if err != nil {
		t.Fatalf("LoadCheck: %v", err)
	}
	if cfg.Language != "fortran" {
		t.Errorf("language = %q, want fortran", cfg.Language)
	}
	if !cfg.AllowWarnings {
		t.Error("allow_warnings from file was not applied")
	}
	if cfg.Timeout != 90*time.Second {
		t.Errorf("timeout = %v, want 90s", cfg.Timeout)
	}
}

func TestLoadCheck_MissingConfigFileErrors(t *testing.T) {
	if _, err := LoadCheck(checkFlags(), "/nonexistent/path/does-not-exist.yaml"); err == nil {
		t.Error("LoadCheck did not error on a nonexistent --config path")
	}
}

func TestLoadCheck_HyphenatedFlagMapsToUnderscoredKey(t *testing.T) {
	fs := checkFlags()
	if err := fs.Set("allow-warnings", "true"); err != nil {
		t.Fatal(err)
	}
	if err := fs.Set("log-dir", "/tmp/custom-log"); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadCheck(fs, "")
	if err != nil {
		t.Fatalf("LoadCheck: %v", err)
	}
	if !cfg.AllowWarnings {
		t.Error("--allow-warnings did not map to allow_warnings")
	}
	if cfg.LogDir != "/tmp/custom-log" {
		t.Errorf("--log-dir did not map to log_dir, got %q", cfg.LogDir)
	}
}

func releaseFlags() *pflag.FlagSet {
	fs := pflag.NewFlagSet("release-build", pflag.ContinueOnError)
	fs.String("module-dir", ".", "")
	fs.String("output-dir", "dist", "")
	fs.String("binary-name", "language-tools", "")
	fs.String("version", "dev", "")
	fs.StringSlice("target", DefaultTargets, "")
	fs.Duration("timeout", 0, "")
	return fs
}

func TestLoadRelease_Defaults(t *testing.T) {
	cfg, err := LoadRelease(releaseFlags(), "")
	if err != nil {
		t.Fatalf("LoadRelease: %v", err)
	}
	if cfg.OutputDir != "dist" || cfg.BinaryName != "language-tools" || cfg.Version != "dev" {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	if len(cfg.Targets) != len(DefaultTargets) {
		t.Errorf("targets = %v, want %v", cfg.Targets, DefaultTargets)
	}
}

func TestLoadRelease_FlagOverridesTargets(t *testing.T) {
	fs := releaseFlags()
	if err := fs.Set("target", "linux/amd64"); err != nil {
		t.Fatal(err)
	}
	if err := fs.Set("target", "windows/amd64"); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadRelease(fs, "")
	if err != nil {
		t.Fatalf("LoadRelease: %v", err)
	}
	if len(cfg.Targets) != 2 || cfg.Targets[0] != "linux/amd64" || cfg.Targets[1] != "windows/amd64" {
		t.Errorf("targets = %v, want [linux/amd64 windows/amd64]", cfg.Targets)
	}
}
