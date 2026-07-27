// Package config loads language-tools' per-command settings through koanf,
// layered flag > env > file > default. It never writes a config file back —
// load-only, same as every clikit CLI's config contract.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/johnrichter/claude-shared-tooling/go/toolchain"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
)

// EnvPrefix namespaces every environment variable language-tools reads, so
// it never collides with an unrelated var of the same short name.
const EnvPrefix = "LANGUAGE_TOOLS_"

// DefaultLogDir is where a check run's uncapped diagnostic and tool-output
// log is written when neither a flag, env var nor config file overrides it.
const DefaultLogDir = ".language-tools/log"

// Check is one build/test/lint invocation's resolved settings.
type Check struct {
	Language      string        `koanf:"language"`
	Dir           string        `koanf:"dir"`
	LogDir        string        `koanf:"log_dir"`
	CacheDir      string        `koanf:"cache_dir"`
	AllowWarnings bool          `koanf:"allow_warnings"`
	Timeout       time.Duration `koanf:"timeout"`
}

// checkDefaults seeds the layering's floor: every setting Check can carry,
// at the value used when nothing else provides it.
var checkDefaults = map[string]any{
	"language":       "",
	"dir":            "",
	"log_dir":        DefaultLogDir,
	"cache_dir":      "",
	"allow_warnings": false,
	"timeout":        toolchain.DefaultTimeout,
}

// LoadCheck resolves a Check from, in ascending priority: the built-in
// defaults, configFile (if non-empty), the LANGUAGE_TOOLS_* environment
// variables, and finally flags — a flag the caller actually set always
// wins, per the flag > env > file > default contract.
func LoadCheck(flags *pflag.FlagSet, configFile string) (*Check, error) {
	var cfg Check
	if err := loadLayered(flags, configFile, checkDefaults, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Release is one release-build invocation's resolved settings.
type Release struct {
	ModuleDir  string        `koanf:"module_dir"`
	OutputDir  string        `koanf:"output_dir"`
	BinaryName string        `koanf:"binary_name"`
	Version    string        `koanf:"version"`
	Targets    []string      `koanf:"target"`
	Timeout    time.Duration `koanf:"timeout"`
}

// DefaultTargets is the fleet's default per-OS/arch build matrix, in
// "os/arch" form (Go's own GOOS/GOARCH pairing).
var DefaultTargets = []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64"}

var releaseDefaults = map[string]any{
	"module_dir":  ".",
	"output_dir":  "dist",
	"binary_name": "language-tools",
	"version":     "dev",
	"target":      DefaultTargets,
	"timeout":     5 * time.Minute,
}

// LoadRelease resolves a Release with the same flag > env > file > default
// layering LoadCheck uses.
func LoadRelease(flags *pflag.FlagSet, configFile string) (*Release, error) {
	var cfg Release
	if err := loadLayered(flags, configFile, releaseDefaults, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// loadLayered runs the shared four-tier load — defaults, file, env, flags —
// and unmarshals the result into out.
func loadLayered(flags *pflag.FlagSet, configFile string, defaults map[string]any, out any) error {
	k := koanf.New(".")
	if err := load(k, confmap.Provider(defaults, "."), nil, "defaults"); err != nil {
		return err
	}
	if configFile != "" {
		if err := load(k, file.Provider(configFile), yaml.Parser(), configFile); err != nil {
			return err
		}
	}
	if err := load(k, env.Provider(EnvPrefix, ".", envKey), nil, "environment"); err != nil {
		return err
	}
	if err := load(k, flagProvider(flags, k), nil, "flags"); err != nil {
		return err
	}
	if err := k.Unmarshal("", out); err != nil {
		return fmt.Errorf("config: unmarshal: %w", err)
	}
	return nil
}

func load(k *koanf.Koanf, p koanf.Provider, pa koanf.Parser, source string) error {
	if err := k.Load(p, pa); err != nil {
		return fmt.Errorf("config: load %s: %w", source, err)
	}
	return nil
}

// envKey maps LANGUAGE_TOOLS_LOG_DIR to the koanf key log_dir, and drops
// (returns "" for) any variable outside the prefix.
func envKey(s string) string {
	if !strings.HasPrefix(s, EnvPrefix) {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(s, EnvPrefix))
}

// flagProvider reads flags into koanf keys using each flag's own typed
// value (posflag.FlagVal), translating the CLI's hyphenated names (--log-dir)
// to the underscored koanf/env keys (log_dir) both share.
func flagProvider(flags *pflag.FlagSet, k *koanf.Koanf) *posflag.Posflag {
	return posflag.ProviderWithFlag(flags, ".", k, func(f *pflag.Flag) (string, any) {
		return strings.ReplaceAll(f.Name, "-", "_"), posflag.FlagVal(flags, f)
	})
}
