// Command language-tools composes the shared toolchain, clikit and sysops
// libraries into one CLI: per-language build/test/lint runs and this
// binary's own per-OS/arch release-build orchestration.
package main

import (
	"os"

	"github.com/johnrichter/claude-shared-tooling/go/toolchain"
	"github.com/johnrichter/language-tools/cmd"
	"github.com/johnrichter/language-tools/internal/lint"
)

// main registers the Go adapter before it runs anything. go/toolchain ships
// that adapter but takes no analyzer dependency of its own, so it registers
// no Go language itself: the driver carrying the analyzer set is built here
// and handed to the adapter this binary registers.
func main() {
	toolchain.Register(toolchain.NewGoAdapter(lint.New()))
	os.Exit(cmd.Execute())
}
