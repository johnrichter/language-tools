// Command language-tools composes the shared toolchain, clikit and sysops
// libraries into one CLI: per-language build/test/lint runs and this
// binary's own per-OS/arch release-build orchestration.
package main

import (
	"os"

	"github.com/johnrichter/language-tools/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
