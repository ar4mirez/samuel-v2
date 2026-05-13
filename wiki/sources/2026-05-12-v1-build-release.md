---
title: v1 Build + Release infrastructure
type: source
created: 2026-05-12
updated: 2026-05-12
sources: []
tags: [v1, build, release]
---

# v1 Build + Release

Ingest pass 11. The Makefile, goreleaser config, GitHub Actions, and install script.

## Files

- `samuel_v1/Makefile` (176 lines) — dev/build/release targets
- `samuel_v1/.goreleaser.yaml` (150 lines) — release config
- `samuel_v1/install.sh` (~200 lines) — POSIX install script
- `samuel_v1/.github/workflows/ci.yml` (~94 lines) — test + lint + multi-platform build
- `samuel_v1/.github/workflows/release.yml` (~67 lines) — tag-triggered release
- `samuel_v1/.github/workflows/docs.yml` — mkdocs deploy
- `samuel_v1/.github/RELEASE_CHECKLIST.md` — manual release checklist

## Key claims

### Makefile

Clean. Standard targets:

- `build` — single platform, with LDFLAGS injecting Version/Commit/BuildDate from git
- `build-all` — cross-compile to darwin/linux/windows × amd64/arm64 (minus windows/arm64)
- `install` / `uninstall` — `/usr/local/bin/samuel`
- `test` / `test-coverage` — `go test -v -race -cover`
- `lint` — `golangci-lint run ./...` (with fallback help if not installed)
- `fmt` — `gofmt -w -s .`
- `deps` — `go mod download && go mod tidy`
- `run` — `go run ./cmd/samuel $(ARGS)` for dev iteration
- `docs` / `docs-serve` — mkdocs build / serve
- `release-dry` / `release` — goreleaser snapshot / release
- `help` — comprehensive target listing

LDFLAGS pattern:
```
-X github.com/ar4mirez/samuel/internal/commands.Version=$(VERSION)
-X github.com/ar4mirez/samuel/internal/commands.Commit=$(COMMIT)
-X github.com/ar4mirez/samuel/internal/commands.BuildDate=$(BUILD_DATE)
```

Build-time injection into [[entities/command-tree-v1]] `Version` / `Commit` / `BuildDate` constants.

### `.goreleaser.yaml`

Solid config. Notable choices:

- **`CGO_ENABLED=0`** — static binary, runs anywhere.
- **5 platforms**: linux/darwin × amd64/arm64, plus windows/amd64. Excludes windows/arm64.
- **`ldflags: -s -w`** — strip debug info, smaller binary.
- **Archive format**: tar.gz for Unix, zip for Windows.
- **Conventional commits → grouped changelog**: Features (^feat) / Bug Fixes (^fix) / Performance (^perf) / Refactoring (^refactor) / Other. Filters out docs/test/ci/chore/Merge commits.
- **Homebrew tap publishing** to `ar4mirez/homebrew-tap/Formula/samuel.rb`. Auto-generates shell completions via `generate_completions_from_executable` (Cobra's `samuel completion` command).
- **`replace_existing_artifacts: true`** — a hard-earned lesson with inline comment:
  > "Caught during the v3.0.0 ship: the first run uploaded assets fine but failed at the homebrew tap step (auth issue). The retry could not proceed past asset upload until this flag was added."
- **Snapshot version template**: `{{ incpatch .Version }}-next` — for non-tag builds.
- **Release header** includes Homebrew + curl install snippets ready to copy.

### GitHub Actions

**`ci.yml`** — triggers on push to main + PRs. Three jobs:
- `test` — Go 1.21, race detector, Codecov upload (non-blocking).
- `lint` — golangci-lint latest.
- `build` — matrix across darwin/linux/windows × amd64/arm64 (excluding windows/arm64). Output to `/dev/null` (just verifying it builds).

Path filters limit runs to Go-relevant changes — saves CI minutes on doc-only PRs.

**`release.yml`** — triggers on tags `v*`. Three jobs:
- `test` — pre-release verification.
- `release` — GoReleaser via `goreleaser/goreleaser-action@v6`. Uses `GITHUB_TOKEN` + `HOMEBREW_TAP_GITHUB_TOKEN` secrets.
- `docs` — triggers a separate docs workflow via `repository-dispatch`.

**`docs.yml`** — deploys mkdocs to GitHub Pages on push to main or release dispatch.

### `install.sh`

POSIX-compliant shell script. Pattern:

```
1. detect_os (linux/darwin/windows via uname -s)
2. detect_arch (amd64/arm64 via uname -m)
3. get_latest_version (GitHub API, curl or wget fallback)
4. download archive to /tmp
5. extract + chmod +x
6. install to $INSTALL_DIR (default /usr/local/bin)
7. verify with `samuel version`
```

UX details that survived:
- Color output via ANSI codes — matches the six-symbol vocabulary in [[entities/ui-package]] (✓✗⚠→).
- Curl/wget fallback (the standard POSIX install-script pattern).
- `INSTALL_DIR` env var override.
- Friendly error messages with install hints.

## Assessment

- **Credibility**: high.
- **Quality**: this is well-engineered, off-the-shelf-shaped release infrastructure. Nothing exotic. Easy to maintain.

## v2 implications

### `#rescue` — port nearly verbatim

- **Makefile** — same targets, retarget paths to `samuel/`.
- **`.goreleaser.yaml`** — same shape. Update `project_name`, `release.github.name`, repository owner if needed.
- **GitHub Actions** — same workflows. Update Go version to whatever ships in 2026 (1.24+ likely).
- **`install.sh`** — same script. Update `GITHUB_REPO` if the v2 repo lives elsewhere.

### `#add` — release hygiene improvements for v2

Per the decisions filed earlier, add to v2's release workflow:

1. **Cosign signing** ([[concepts/versioning-compatibility]] decision) — sign release archives + framework binary with cosign keyless. Adds ~30 seconds to release workflow.
   ```yaml
   - uses: sigstore/cosign-installer@v3
   - run: cosign sign-blob --bundle samuel.bundle samuel
   ```

2. **OCI image publication** — push framework as `ghcr.io/ar4mirez/samuel:vX.Y.Z`. Lets users run `docker run ghcr.io/ar4mirez/samuel run ralph` for ephemeral use.
   ```yaml
   - uses: docker/build-push-action@v5
     with:
       tags: ghcr.io/ar4mirez/samuel:${{ github.ref_name }}
       push: true
   ```

3. **SBOM generation** — `syft` or `goreleaser` built-in SBOM. SLSA Level 2+ provenance.
   ```yaml
   sboms:
     - artifacts: archive
   ```

4. **AGENTS.md template length check** — CI fails if template exceeds hard limit (RFD 0001 lesson from [[sources/2026-05-12-v1-rfds]]).
   ```yaml
   - name: AGENTS.md template line check
     run: |
       lines=$(wc -l < samuel/template/AGENTS.md.tmpl)
       if [ "$lines" -gt 150 ]; then
         echo "::error::AGENTS.md template is $lines lines (limit: 150). See RFD 0001."
         exit 1
       fi
   ```

5. **Plugin registry validation** — for the v2 `samuel-registry` repo, a CI check that every referenced plugin actually resolves.

### `#refactor`

- **Multi-arch OCI image build**: linux/amd64 + linux/arm64. Use `docker buildx` in the workflow.
- **Cosign tap publishing**: extend the homebrew tap step to also publish a cosigned formula. Brew's `cask-verifier` can validate the signature.
- **Plugin release workflow template** — every plugin repo gets a copy of a shared release workflow that builds (TinyGo for WASM, multi-arch for OCI), signs, and publishes. Ship as a `samuel-plugin-release.yml` reusable workflow.

### `#drop`

- Nothing major in this pass. The build/release infrastructure ages well.

## Resolved

- **No `samuel.dev` domain yet** (#v2-decision 2026-05-12). v2 stays on `ar4mirez.github.io/samuel/`. Rewrite v2 error DocsURLs to github.io URLs to avoid dead links. Domain registration deferred.

## Open

- **Codecov is non-blocking** in v1 CI. Keep it that way — coverage drift shouldn't block PRs.
- **Lint config** (`.golangci.yml`) wasn't read — should be checked in pass 12 (project meta).

## Related pages

- [[concepts/versioning-compatibility]] — Sigstore/SLSA decisions for v2 release hygiene
- [[entities/command-tree-v1]] — where Version/Commit/BuildDate flow
- [[sources/2026-05-12-v1-rfds]] — RFD 0001's lesson on enforcing line limits
