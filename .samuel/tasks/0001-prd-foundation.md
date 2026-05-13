---
prd: "0001"
milestone: "Foundation"
title: Samuel v2 Foundation — scaffold, encoding, ported utilities, CI
authors:
  - name: ar4mirez
state: Draft
labels: [v2, foundation, build-infra, toon]
created: 2026-05-12
updated: 2026-05-12
target_release: v2.0.0-alpha.1
estimated_effort: 2-3 weeks
---

# PRD 0001: Samuel v2 Foundation

## Wiki references

- [[concepts/toon-evaluation]] — the TOON adoption decision and rationale
- [[entities/config-format]] — TOML for human config, TOON for runtime structured, YAML for SKILL.md
- [[entities/orchestrator]] — errors + lock pattern to port
- [[concepts/structured-errors]] — `Error{Problem,Cause,Fix,DocsURL}` pattern
- [[sources/2026-05-12-v1-build-release]] — v1 Makefile, goreleaser, install.sh patterns to inherit

## Summary

Establish v2's repository scaffold, encoding layer (TOON), ported utility packages (errors, lock), and build/release infrastructure. This milestone produces a working Go module that compiles to a binary printing `samuel version` — nothing more — but with every cross-cutting subsystem the rest of the milestones depend on already in place.

## Problem statement

v2 is a clean break from v1, but ~70% of v1's code ports forward. The risk in starting code without scaffolding is two-fold: (a) cross-cutting concerns (errors, locking, encoding, CI) get reinvented per-milestone with drift, and (b) the project lacks a working binary to run smoke tests against until much later.

Foundation milestone defuses both by landing the shared infrastructure first.

## Goals

- v2 Go module compiles, runs, and emits `samuel version` with build-time-injected version/commit/date.
- TOON encoder/decoder works for v2's data shapes (tasks list, file inventory, key-value maps).
- Structured `Error` type ported from v1 with same shape (`Problem/Cause/Fix/DocsURL/Recoverable/Path`).
- flock(2) advisory lock at `~/.samuel/lock` works on Linux/macOS, no-op fallback on Windows.
- Makefile, goreleaser config, GitHub Actions (CI + release), install.sh — all working against v2's structure.
- Empty plugin loader interface defined (no implementations yet) — so Milestone 2 can wire to it.
- `samuel.toml` schema defined and serialized via `pelletier/go-toml v2`.
- AGENTS.md template at ≤150 lines, with CI check that fails the build if exceeded.

## Non-goals

- No `samuel init`, `samuel install`, `samuel run` commands yet (Milestone 2 + 3 + 4).
- No methodology implementations (Milestone 4).
- No plugin registry fetching (Milestone 3).
- No skill content migration (Milestone 5).
- No documentation site beyond a placeholder (Milestone 6).

## Requirements

### Functional

1. **Go module scaffold** at repo root: `go.mod`, `cmd/samuel/main.go`, `internal/` subtree.
2. **TOON encoder/decoder** at `internal/encoding/toon/`:
   - Encode/decode the v3 spec subset Samuel actually uses (tabular arrays, nested objects, primitives, comments at minimum).
   - Pin spec version `3.0` in code; reject unknown major versions on read.
   - Version header on each `.toon` file: `# toon v3` as first line.
   - Per-row malformation recovery: skip a bad row with a structured warning, continue parsing.
   - Round-trip tests against a golden corpus.
3. **Errors package** at `internal/errors/`:
   - Port v1's `*Error` type verbatim (six fields: Component, Problem, Cause, Fix, DocsURL, Recoverable, Path).
   - `Wrap(err)` for chain preservation; `IsRecoverable(err)` for retry decisions.
   - Error code namespace: `SAM-<area>-<NNN>` (e.g., `SAM-LOCK-001`).
4. **Lock package** at `internal/lock/`:
   - flock(2) at `~/.samuel/lock`, `O_CLOEXEC` on fd.
   - PID write for diagnostics, validated read with 32-byte cap.
   - `//go:build unix` for real impl, no-op fallback for other.
5. **Config package** at `internal/config/`:
   - `samuel.toml` read/write via `pelletier/go-toml v2`.
   - Schema: `version`, `default_methodology`, `[[plugins]]`, `[methodology.<name>]`, `[guardrails]`, `[[registries]]`.
   - `samuel.lock` read/write (also TOML).
6. **Plugin interface stub** at `internal/plugin/`:
   - `Plugin` interface (Name, Manifest, Detect, Install, Check, Uninstall).
   - `Manifest` struct matching the `samuel-plugin.toml` schema.
   - Three empty plugin-kind structs (`SkillPlugin`, `WasmPlugin`, `OciPlugin`) — implementations land in Milestone 3.
7. **CLI scaffold** at `internal/commands/`:
   - Cobra root command with `version`, `--json`, `--no-color`, `-v`, `--no-deprecation` persistent flags.
   - `JSONMode(cmd)` helper.
   - JSON envelope (`schemaVersion: 4`) for `--json` output.
8. **`samuel version`** prints version/commit/date (build-time injected via LDFLAGS).
9. **Charm UI base** at `internal/ui/`:
   - `lipgloss` color tokens (success/error/warn/info/bold/dim).
   - JSON envelope helper.
   - Spinner from `bubbles/spinner` (used by Milestone 2+ commands).
10. **AGENTS.md template** at `template/AGENTS.md.tmpl` — ≤150 lines.

### Non-functional

- Go 1.24+ target.
- All packages have `_test.go` coverage; aim for ≥80% on `internal/{errors,lock,encoding/toon,config}`.
- All ports preserve security primitives: path traversal defense, size caps, atomic save (write-tmp-then-rename).
- No external network calls (no plugin fetching yet).
- Linux + macOS supported; Windows builds (no flock, gracefully).

### Build / release

- **Makefile** ports v1's targets (`build`, `build-all`, `test`, `lint`, `fmt`, `deps`, `install`, `uninstall`, `clean`, `version`).
- **`.goreleaser.yaml`** ports v1's config with v2 owner/repo names. Add cosign signing step ([[concepts/versioning-compatibility]]).
- **GitHub Actions** at `.github/workflows/`:
  - `ci.yml` — test, lint, multi-platform build matrix.
  - `release.yml` — tag-triggered goreleaser + cosign.
  - `agents-md-check.yml` — fails if `template/AGENTS.md.tmpl` > 150 lines (RFD 0001 lesson).
- **install.sh** — POSIX shell, OS/arch detection, curl/wget fallback. Port from v1.

## Implementation approach

Bottom-up build order:

1. **Go module + Makefile** — skeleton compiles, `make build` produces a binary.
2. **Errors package** — port from `samuel_v1/internal/orchestrator/errors.go`.
3. **Lock package** — port from `samuel_v1/internal/orchestrator/lock_unix.go` + `lock_other.go`.
4. **TOON encoder/decoder** — audit existing Go libraries; write our own if none maintained. Spec at `.wiki/concepts/toon-evaluation.md`.
5. **Config package** — TOML schema + read/write.
6. **CLI root + version command** — Cobra setup, JSON envelope (`schemaVersion: 4`), `--json` flag.
7. **Charm UI base** — lipgloss tokens, JSON helpers.
8. **AGENTS.md template** at ≤150 lines + CI check.
9. **goreleaser + GitHub Actions** — copy v1 config, retarget to v2 paths, add cosign step.
10. **install.sh** — copy v1, update URLs.

## Acceptance criteria

- [x] `go build ./...` succeeds.
- [x] `samuel version` prints version, commit, build date.
- [x] `samuel version --json` emits valid JSON envelope at `schemaVersion: 4`.
- [x] TOON encoder round-trips a 60-task fixture matching the v1 dogfood `prd.json` shape (translated). _Evidence: `TestRoundTrip_Golden/prd-60-tasks.toon` + `TestPRD60Tasks_FixtureCount`._
- [x] TOON decoder gracefully skips a malformed row, emits structured warning. _Evidence: `TestUnmarshal_MalformedRowRecovery`._
- [x] flock(2) on `~/.samuel/lock` blocks concurrent samuel processes. _Evidence: `TestAcquire_LiveLockReturnsBusy` + `TestAcquire_ConcurrentSerializes`._
- [x] Structured `*Error` renders multi-line in CLI, single-line in logs. _Evidence: `TestRenderError_MultiLineForStructured` (multi-line) + `TestError_FormatsWithCause` (single-line)._
- [x] `samuel.toml` round-trips without losing any field. _Evidence: `TestSaveLoad_RoundTrip` (reflect.DeepEqual)._
- [x] AGENTS.md template is ≤150 lines. _Source 104 lines, max-config render 90 lines, mirrored by `TestAgentsMDTemplate_LineBudget` + CI `agents-md-check.yml`._
- [x] CI runs: test, lint, multi-platform build, AGENTS.md line check. _All four jobs green on commits `eb2f828` and `ab8d589`._
- [x] `goreleaser release --snapshot --clean` produces signed artifacts locally. _Snapshot builds all 4 archives + checksums in ~1s. Cosign keyless signing is CI-only (requires OIDC); local snapshots run with `--skip=sign`. The actual signed artifacts were produced by the v2.0.0-alpha.1 release workflow and verified end-to-end with `cosign verify-blob` against the Actions OIDC issuer (both darwin_arm64 and linux_amd64 returned "Verified OK")._
- [x] `install.sh` installs the resulting binary on a clean macOS/Linux container. _macOS arm64 verified: `install.sh` into a fresh `/tmp/samuel-alpha-smoke/`, executed, reported v2.0.0-alpha.1. Linux amd64 verified at the artifact level: download, SHA256 match, ELF x86-64 statically linked + stripped, cosign verify OK; container exec deferred — no container runtime available on the dev machine._

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| No maintained Go TOON library exists | High | Write our own. Spec is small (~28 formatting rules in v3). Plan 3 days for impl + tests. |
| TOON spec v4 lands during build | Low | Pin v3 in code. Migration handled when v4 stabilizes. |
| `~/.samuel/lock` collides with v1's `~/.claude/.samuel.lock` | Medium | Different path, different process — confirmed no collision. Document for users running v1 + v2 side by side. |
| Cosign keyless signing requires GitHub Actions OIDC token | Low | Standard pattern, well-documented. Use `sigstore/cosign-installer@v3`. |
| AGENTS.md template grows past 150 lines while writing | Medium | CI check fails the PR. Hard limit enforced from day 1. |

## Open questions

- **Repo strategy**: build in `samuel_v2/` locally, then force-push to `github.com/samuelpkg/samuel` on launch? Or new repo `github.com/samuelpkg/samuel-v2`? Recommend the force-push approach to preserve install URLs.
- **Cosign signing default**: signed-by-default for the official registry, `--allow-unsigned` for dev? Confirm.
- **JSON envelope schema bump**: `schemaVersion: 4`. Document v3 → v4 changes in code comment per v1's pattern.

## Task hints

Atomic-sized subtasks for generate-tasks:

1. `go mod init github.com/samuelpkg/samuel`
2. Create `cmd/samuel/main.go` stub
3. Port `errors.go` from v1 with namespace renamed
4. Add `errors_test.go` with full coverage
5. Port `lock_unix.go` from v1
6. Port `lock_other.go` (Windows fallback)
7. Add `lock_unix_test.go` cross-process test
8. Audit Go TOON libs; decision in 1-page memo
9. Build `internal/encoding/toon/encoder.go` (skill plugins, tabular arrays, primitives)
10. Build `internal/encoding/toon/decoder.go` with malformation recovery
11. Build TOON golden-corpus test fixture
12. Define `Config` struct + TOML schema
13. Build `internal/config/load.go` + `save.go`
14. Add `config_test.go` round-trip suite
15. Define `Plugin` interface + `Manifest` struct
16. Cobra root command + persistent flags
17. `version` command + JSON envelope at schemaVersion 4
18. Charm `lipgloss` color tokens
19. AGENTS.md template draft + ≤150 line CI check
20. Makefile (port v1)
21. `.goreleaser.yaml` (port v1, add cosign)
22. GitHub Actions: `ci.yml`, `release.yml`, `agents-md-check.yml`
23. `install.sh` (port v1)
24. Tag `v2.0.0-alpha.1` and verify release artifacts
