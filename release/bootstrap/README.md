# Bootstrap Release Procedure

This directory documents the manual first release procedure for language-tools — the chicken-and-egg case where language-tools cannot yet build itself and must be built manually.

## Overview

The first release (v1.0.0) was bootstrapped manually because the release-build orchestration runs inside the language-tools binary itself (`language-tools release build`). Subsequent releases use the same procedure but execute through the pre-built language-tools binary, making them self-hosted.

## Procedure

### 1. Ensure Code is Ready

The code must pass all CI checks (gofmt, go vet, go build, go test, golangci-lint):

```sh
gofmt -l .
go vet ./...
go build -trimpath -ldflags='-s -w' -o language-tools .
go test ./...
golangci-lint run
```

Verify no committed binaries exist:

```sh
bash release/guard/no-committed-binaries.sh .
```

### 2. Build the Language-Tools Binary

```sh
go build -trimpath -ldflags='-s -w' -o language-tools .
```

This binary is used only during bootstrap; it will not be committed (guarded by SC-DISTRIBUTION).

### 3. Build Release Archives and Checksums

Run the release-build command, which cross-compiles the binary for all targets and produces per-OS/arch tarballs:

```sh
mkdir -p dist
./language-tools release build --version v1.0.0 --output-dir dist
```

Output:
- `dist/language-tools_v1.0.0_linux_amd64.tar.gz`
- `dist/language-tools_v1.0.0_linux_arm64.tar.gz`
- `dist/language-tools_v1.0.0_darwin_amd64.tar.gz`
- `dist/language-tools_v1.0.0_darwin_arm64.tar.gz`
- `dist/checksums.txt` (SHA256 manifest for all archives)

### 4. Verify Checksums

```sh
cd dist && sha256sum -c checksums.txt
```

All archives must verify as OK.

### 5. Tag the Release

Apply the release tag to the commit from which archives were built:

```sh
git tag -a v1.0.0 -m "language-tools v1.0.0: initial release"
```

Verify the tag:

```sh
git tag -l v1.0.0
git show v1.0.0
```

Ensure the tag-prefix conforms to SC-VERSIONING:

```sh
bash release/guard/tag-prefix.sh v1.0.0
```

Exit code 0 confirms conformance.

### 6. Publish the Release (Separate Step)

Once the tag is created and verified locally, publish the GitHub release:

```sh
gh release create v1.0.0 --title v1.0.0 --generate-notes dist/*.tar.gz dist/checksums.txt
```

Or trigger the `release.yml` workflow manually via GitHub Actions with `workflow_dispatch` if preferred.

## Subsequent Releases

From v1.1.0 onward, the release process becomes self-hosted:

1. Ensure code is ready (CI checks, guards).
2. Use the pre-built language-tools binary to create the next release:
   ```sh
   language-tools release build --version v1.1.0 --output-dir dist
   ```
3. Verify checksums.
4. Tag the commit.
5. Publish via `gh release create` or `release.yml` workflow.

The pre-built binary is obtained via the normal artifact-download pipeline (GitHub release assets), so subsequent releases do not depend on a local build of language-tools.

## Conformance

- **SC-VERSIONING** (`release/guard/tag-prefix.sh`): Release tags must match the form `vX.Y.Z` with no module prefix (language-tools is a single root module).
- **SC-DISTRIBUTION** (`release/guard/no-committed-binaries.sh`): The built binary and archives must never be committed; all artifacts ship via GitHub releases.
- **PROD-BAR**: Archives are reproducible (Go cross-compilation, trimpath, ldflags), checksums are SHA256-verifiable, and the tag is cryptographically signed (if enforced).

## Troubleshooting

- **gofmt failures**: Run `gofmt -w .` to auto-format.
- **go vet failures**: Investigate the vet output; most are real bugs.
- **golangci-lint failures**: Review linter rules in `.golangci.yml` and fix violations.
- **Checksum mismatch**: Ensure no file was modified after `language-tools release build`. Rebuild if necessary.
- **Tag already exists**: Delete the local tag with `git tag -d v1.0.0` and re-create, or use a new version number.
