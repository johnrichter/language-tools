package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/johnrichter/claude-shared-tooling/go/toolchain"
	"github.com/johnrichter/language-tools/internal/config"
	"github.com/johnrichter/language-tools/internal/pin"
	"github.com/johnrichter/language-tools/internal/result"
	"github.com/spf13/cobra"
)

// newCheckCmd builds the build/test/lint/format/vet subcommand for check:
// one thin wrapper over pin.Check and toolchain.Run shared by all of them,
// since the only thing that differs between them is which check toolchain
// runs. pin.Check always runs first: a pin that fails to hold gates the run
// before the tool it would have invoked ever starts.
func newCheckCmd(check toolchain.Check) *cobra.Command {
	cmd := &cobra.Command{
		Use:     string(check),
		Short:   fmt.Sprintf("Run %s through the toolchain adapter registered for --language", check),
		Example: fmt.Sprintf("  language-tools %s --language rust --dir ./crates/example", check),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck(cmd, check)
		},
	}
	cmd.Flags().String("language", "", "language adapter to run (e.g. rust)")
	cmd.Flags().String("dir", "", "target crate/module/package directory")
	cmd.Flags().String("log-dir", config.DefaultLogDir, "directory for this run's uncapped diagnostic + tool-output log")
	cmd.Flags().String("cache-dir", "", "directory for the content-hash impact-skip cache (empty disables caching)")
	cmd.Flags().Bool("allow-warnings", false, "let a warning-only run classify as success (default: warnings fail the run)")
	cmd.Flags().Duration("timeout", toolchain.DefaultTimeout, "wall-clock bound on the underlying tool invocation")
	return cmd
}

func runCheck(cmd *cobra.Command, check toolchain.Check) error {
	configFile, _ := cmd.Flags().GetString("config")
	if configFile != "" {
		if err := statUsable(configFile); err != nil {
			return finishUsage(cmd, "usage.cli.config_not_found", fmt.Sprintf("--config %s: %s", configFile, err))
		}
	}
	cfg, err := config.LoadCheck(cmd.Flags(), configFile)
	if err != nil {
		return finishErr(cmd, "internal.config.load_failed", "load configuration", err)
	}
	if cfg.Language == "" {
		return finishUsage(cmd, "usage.cli.missing_language", "--language (or LANGUAGE_TOOLS_LANGUAGE, or config key language) is required")
	}
	if cfg.Dir == "" {
		return finishUsage(cmd, "usage.cli.missing_dir", "--dir (or LANGUAGE_TOOLS_DIR, or config key dir) is required")
	}
	if err := statDir(cfg.Dir); err != nil {
		return finishUsage(cmd, "usage.cli.dir_not_found", fmt.Sprintf("--dir %s: %s", cfg.Dir, err))
	}

	target := toolchain.Target{
		Language: cfg.Language,
		Check:    check,
		Dir:      cfg.Dir,
	}

	gate, err := pin.Check(cmd.Context(), commandPath(cmd), target)
	if err != nil {
		return finishErr(cmd, "internal.pin.check_failed", "check toolchain pin", err)
	}
	if gate != nil {
		return finish(cmd, gate)
	}

	run, err := toolchain.Run(cmd.Context(), target, toolchain.Options{
		AllowWarnings: cfg.AllowWarnings,
		LogDir:        cfg.LogDir,
		CacheDir:      cfg.CacheDir,
		Timeout:       cfg.Timeout,
	})
	if err != nil {
		if errors.Is(err, toolchain.ErrUnsupportedCheck) {
			return finishUnsupported(cmd, "unsupported.toolchain.check_not_supported", fmt.Sprintf("%s for language %s: %s", check, cfg.Language, err))
		}
		// toolchain.Run returns an unwrapped error for every other failure
		// mode it has, including an unregistered --language -- a caller
		// mistake, not an infrastructure fault -- and exposes no way to tell
		// them apart from here. Until it does, that one case is
		// misclassified as internal below rather than usage.
		return finishErr(cmd, "internal.toolchain.run_failed", fmt.Sprintf("run %s for language %s", check, cfg.Language), err)
	}

	clikitResult, err := result.FromRun(commandPath(cmd), run)
	if err != nil {
		return finishErr(cmd, "internal.result.map_failed", "translate toolchain result", err)
	}
	return finish(cmd, clikitResult)
}

// statDir reports an error if dir does not exist or is not a directory —
// checked up front so a bad --dir surfaces as a usage mistake rather than
// whatever error the underlying tool exec happens to fail with.
func statDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory")
	}
	return nil
}

// statUsable reports an error if path does not exist or is a directory —
// checked up front so a bad --config surfaces as a usage mistake rather than
// the koanf file provider's own load error.
func statUsable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("is a directory, not a file")
	}
	return nil
}
