// Package cmd wires language-tools' cobra command tree onto the shared
// toolchain, clikit and release libraries. Every command emits exactly one
// clikit.Result to stdout and exits with that result's exit code — cobra's
// own usage/error printing is silenced so it never competes with that one
// record.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/johnrichter/claude-shared-tooling/go/clikit"
	"github.com/johnrichter/language-tools/internal/result"
	"github.com/spf13/cobra"
)

// exitError carries a clikit-derived exit code up through cobra's error
// return path without cobra printing anything itself — the command that
// raised it has already emitted its clikit.Result.
type exitError struct{ code int }

func (e *exitError) Error() string { return fmt.Sprintf("exit code %d", e.code) }

// newRootCmd builds the command tree: build/test/lint, each a thin
// toolchain.Run wrapper, plus release build for this binary's own
// per-OS/arch distribution.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "language-tools",
		Short: "Per-language build/test/lint and this CLI's own release-build orchestration",
		Long: `language-tools composes the shared toolchain, clikit and sysops libraries
into one CLI: build, test and lint any toolchain-registered language through
a single bounded RunResult, and cross-compile this binary into checksummed
per-OS/arch release archives.`,
		Example: strings.TrimLeft(`
  language-tools build --language rust --dir ./crates/example
  language-tools test --language rust --dir ./crates/example --allow-warnings
  language-tools release build --version v1.2.3 --output-dir dist
`, "\n"),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().String("config", "", "path to a YAML config file (flag > env > file > default)")
	root.AddCommand(newCheckCmd("build"))
	root.AddCommand(newCheckCmd("test"))
	root.AddCommand(newCheckCmd("lint"))
	root.AddCommand(newReleaseCmd())
	return root
}

// Execute runs the command tree and returns the process exit code —
// clikit's, for anything that reached a subcommand, or a usage code for an
// invocation cobra itself rejected before that (e.g. an unknown flag).
func Execute() int {
	root := newRootCmd()
	ranCmd, err := root.ExecuteC()
	if err == nil {
		return 0
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return emitUsageError(ranCmd, err)
}

// commandPath renders cmd's full invocation ("language-tools build") as the
// token slice clikit.Result.Command requires.
func commandPath(cmd *cobra.Command) []string {
	return strings.Fields(cmd.CommandPath())
}

// emitUsageError handles an error cobra raised before any subcommand's RunE
// ran (bad flag, unknown subcommand) — no clikit.Result has been emitted
// yet, so this is the one place that builds one for that case.
func emitUsageError(cmd *cobra.Command, err error) int {
	diag, buildErr := clikit.NewError(
		"usage.cli.invalid_invocation",
		result.OneLine(err.Error()),
		clikit.Manual("run `language-tools --help` (or `language-tools <command> --help`) for valid flags and usage"),
		nil,
	)
	if buildErr != nil {
		fmt.Fprintln(os.Stderr, err)
		return clikit.StatusInternal.ExitCode()
	}
	result, buildErr := clikit.NewUsage(commandPath(cmd), nil, []clikit.Diagnostic{diag}, nil)
	if buildErr != nil {
		fmt.Fprintln(os.Stderr, err)
		return clikit.StatusInternal.ExitCode()
	}
	if emitErr := clikit.Emit(os.Stdout, result); emitErr != nil {
		fmt.Fprintln(os.Stderr, emitErr)
	}
	return result.ExitCode
}

// finish emits result and turns it into cobra's error-return path: nil for
// success, an *exitError carrying result.ExitCode otherwise.
func finish(cmd *cobra.Command, result *clikit.Result) error {
	if err := clikit.Emit(cmd.OutOrStdout(), result); err != nil {
		return err
	}
	if result.ExitCode == 0 {
		return nil
	}
	return &exitError{code: result.ExitCode}
}

// finishErr builds and emits a clikit.StatusInternal result for err — an
// infrastructure failure from this CLI itself, not a diagnostic the
// underlying tool reported. code must be in the "internal" class.
func finishErr(cmd *cobra.Command, code, message string, err error) error {
	diag, buildErr := clikit.NewError(code, result.OneLine(fmt.Sprintf("%s: %s", message, err)), clikit.Manual("retry; if this persists, file an issue with the log output"), nil)
	if buildErr != nil {
		return buildErr
	}
	result, buildErr := clikit.NewInternal(commandPath(cmd), nil, []clikit.Diagnostic{diag}, nil)
	if buildErr != nil {
		return buildErr
	}
	return finish(cmd, result)
}

// finishUsage builds and emits a clikit.StatusUsage result: the invocation
// itself is wrong (a required setting missing, an unparseable value) and
// nothing was attempted. code must be in the "usage" class.
func finishUsage(cmd *cobra.Command, code, message string) error {
	diag, buildErr := clikit.NewError(
		code, result.OneLine(message),
		clikit.Manual(fmt.Sprintf("run `%s --help` for valid flags and usage", cmd.CommandPath())),
		nil,
	)
	if buildErr != nil {
		return buildErr
	}
	result, buildErr := clikit.NewUsage(commandPath(cmd), nil, []clikit.Diagnostic{diag}, nil)
	if buildErr != nil {
		return buildErr
	}
	return finish(cmd, result)
}
