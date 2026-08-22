# language-tools

Language-specific tooling and helpers.

## Claude Code setup

This repo's `.claude/settings.json` enables plugins from the `jr-claude-plugins` marketplace. Register it once at the Claude user level — repo settings carry no machine-specific paths:

```sh
claude plugin marketplace add git@github.com:johnrichter/claude-marketplace.git
# or, with the psa-platform repos checked out as siblings:
claude plugin marketplace add ../marketplace-public
```

Knowledge bases are configured at the Claude user level, not per repo.

## language-tools CLI

Cobra+koanf CLI composing the `ai-shared-lib` `toolchain`, `clikit` and `sysops` libraries: per-language build/test/lint/format/vet runs, and this binary's own per-OS/arch release-build orchestration. Every invocation writes one [clikit](../ai-shared-lib/go/clikit) result record to stdout and exits with clikit's exit-code taxonomy (0/10/20/.../90) — never a bare Go error to stderr.

Requires this repo checked out as a sibling of `ai-shared-lib` (`go.mod`'s `replace` directives point at `../ai-shared-lib/go/*`).

### Build

```sh
go build -o language-tools .
```

### Commands

```sh
language-tools build  --language <lang> --dir <path>   # cargo/etc. build, capped RunResult
language-tools test   --language <lang> --dir <path>   # same, for tests
language-tools lint   --language <lang> --dir <path>   # same, for lint
language-tools format --language <lang> --dir <path>   # same, for format
language-tools vet    --language <lang> --dir <path>   # same, for vet
language-tools release build --version <ver> --output-dir <dir>  # this binary's own archives+checksums
```

`build`/`test`/`lint`/`format`/`vet` share `--log-dir` (uncapped diagnostic log, default `.language-tools/log`), `--cache-dir` (content-hash impact-skip cache, disabled by default), `--allow-warnings` (default off — warnings fail the run), and `--timeout`. `release build` defaults `--target` to `linux/amd64,linux/arm64,darwin/amd64,darwin/arm64` and writes a `checksums.txt` manifest alongside the archives.

Every setting is also a `LANGUAGE_TOOLS_<NAME>` environment variable and a YAML `--config` file key, layered flag > env > file > default (e.g. `--allow-warnings` / `LANGUAGE_TOOLS_ALLOW_WARNINGS` / `allow_warnings:` in the config file).

Currently only the `rust` (cargo) toolchain adapter is registered upstream; `build`/`test`/`lint`/`format`/`vet` surface whatever languages `ai-shared-lib/go/toolchain` registers, with no per-language logic duplicated here. A check an adapter doesn't implement (e.g. `vet` on a language without one) exits 80 (`unsupported`), not an internal fault.

### Example

```sh
language-tools build --language rust --dir ./crates/example
language-tools release build --version v1.2.3 --output-dir dist
```
