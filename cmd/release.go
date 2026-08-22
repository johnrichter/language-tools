package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/johnrichter/claude-shared-tooling/go/clikit"
	"github.com/johnrichter/language-tools/internal/config"
	"github.com/johnrichter/language-tools/internal/release"
	"github.com/spf13/cobra"
)

// newReleaseCmd is the release-build orchestration parent: this binary's
// own distribution, not a per-language check.
func newReleaseCmd() *cobra.Command {
	releaseCmd := &cobra.Command{
		Use:   "release",
		Short: "This CLI's own release-build orchestration",
	}
	releaseCmd.AddCommand(newReleaseBuildCmd())
	return releaseCmd
}

func newReleaseBuildCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "build",
		Short:   "Cross-compile this binary into checksummed per-OS/arch archives",
		Example: "  language-tools release build --version 1.2.3 --output-dir dist",
		RunE:    runReleaseBuild,
	}
	cmd.Flags().String("module-dir", ".", "Go module root every binary's package is resolved against")
	cmd.Flags().String("output-dir", "dist", "directory archives and checksum manifests are written to")
	cmd.Flags().StringSlice("binary", config.DefaultBinaries, `"name:package" binary to build (repeatable)`)
	cmd.Flags().String("version", "dev", "version tag embedded in each archive's filename and binary-checksums.txt key (no leading \"v\")")
	cmd.Flags().StringSlice("target", config.DefaultTargets, `build target "os/arch" (repeatable)`)
	cmd.Flags().Duration("timeout", 5*time.Minute, "wall-clock bound on each target's go build invocation")
	return cmd
}

func runReleaseBuild(cmd *cobra.Command, args []string) error {
	configFile, _ := cmd.Flags().GetString("config")
	if configFile != "" {
		if err := statUsable(configFile); err != nil {
			return finishUsage(cmd, "usage.cli.config_not_found", fmt.Sprintf("--config %s: %s", configFile, err))
		}
	}
	cfg, err := config.LoadRelease(cmd.Flags(), configFile)
	if err != nil {
		return finishErr(cmd, "internal.config.load_failed", "load configuration", err)
	}

	targets := make([]release.Target, 0, len(cfg.Targets))
	for _, raw := range cfg.Targets {
		t, err := release.ParseTarget(raw)
		if err != nil {
			return finishUsage(cmd, "usage.cli.invalid_target", err.Error())
		}
		targets = append(targets, t)
	}

	binaries := make([]release.Binary, 0, len(cfg.Binaries))
	for _, raw := range cfg.Binaries {
		b, err := release.ParseBinarySpec(raw)
		if err != nil {
			return finishUsage(cmd, "usage.cli.invalid_binary", err.Error())
		}
		binaries = append(binaries, b)
	}

	artifacts, err := release.Build(cmd.Context(), release.Options{
		ModuleDir: cfg.ModuleDir,
		OutputDir: cfg.OutputDir,
		Binaries:  binaries,
		Version:   cfg.Version,
		Targets:   targets,
		Timeout:   cfg.Timeout,
	})
	if err != nil {
		return finishErr(cmd, "internal.release.build_failed", "build release artifacts", err)
	}

	data := map[string]any{
		"output_dir":     cfg.OutputDir,
		"version":        cfg.Version,
		"artifact_count": len(artifacts),
		"binaries":       strings.Join(cfg.Binaries, ","),
		"targets":        targetList(artifacts),
	}
	successResult, err := clikit.NewSuccess(commandPath(cmd), data)
	if err != nil {
		return finishErr(cmd, "internal.result.build_failed", "build result", err)
	}
	return finish(cmd, successResult)
}

func targetList(artifacts []release.Artifact) string {
	parts := make([]string, len(artifacts))
	for i, a := range artifacts {
		parts[i] = a.Target.String()
	}
	return strings.Join(parts, ",")
}
