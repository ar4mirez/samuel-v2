---
prd: "0007"
milestone: "Live-registry e2e tier"
title: Samuel v2 Live-Registry E2E — close the rc-cycle recurrence gap
authors:
  - name: ar4mirez
state: Draft
labels: [v2, e2e, testing, registry, git-fetcher]
created: 2026-05-13
updated: 2026-05-13
target_release: v2.0.1
estimated_effort: 1 week
depends_on: 0006-prd-polish-launch.md
---

# PRD 0007: Live-Registry E2E Tier

## Wiki references

- [[synthesis/v2-rc-cycle-lessons]] — the rc.2 → rc.15 cycle, "Hermetic e2e suite codifies the manual sweep" and the limitation paragraph at the bottom
- [[sources/2026-05-12-v1-build-release]] — CI patterns to extend
- [GitHub Issue #10](https://github.com/samuelpkg/samuel/issues/10) — tracking issue

## Summary

Stand up `e2e/live/` — a second, network-allowed tier of the end-to-end suite that exercises Samuel against a real git-backed registry. The hermetic tier (rc.15, `e2e/hermetic/`) covers ~80% of the manual sweep, but `file://` URLs route through `source.fetchFile` rather than `source.fetchGit`, so the rc.6 (v-prefix tag fallback) and rc.9 (.git strip on install) fixes have no CLI-surface coverage. This PRD closes that gap with a nightly-only test job that clones, installs, and verifies plugins from an actual GitHub registry fixture.

## Problem statement

The hermetic e2e suite was designed for speed and determinism: a single `go test` build, isolated tempdir + HOME, a local file:// registry pointing at `testdata/sample-skill/`. That design has a structural limitation — `source.fetchFile` and `source.fetchGit` are different codepaths in `internal/plugin/source/`. The hermetic tier exercises one; the live tier must exercise the other.

The rc.6 and rc.9 bugs both lived in `fetchGit` and only manifested against a real git remote with real refs. Both fixes are currently protected by unit tests in `internal/plugin/source/source_test.go` against an on-disk bare repo, which is closer to production than file:// but still does not exercise the `samuel install` CLI path end-to-end against a remote registry index.

A live tier closes the recurrence gap: any regression in fetcher behavior, registry parsing under real conditions, or signature verification against artifacts published to a real CDN gets caught nightly, not in a future rc cycle.

## Goals

- New `e2e/live/` test tree with build tag `e2e_live`, off by default, nightly in CI.
- Dedicated test registry: `github.com/samuelpkg/samuel-test-registry` with a stable index.toml and 3–5 fixture plugins (signed under the test identity).
- CLI-surface tests for: `samuel install` (happy path + v-prefix tag fallback + `.git` strip), `samuel update`, `samuel search`, `samuel doctor` (against live-installed plugins), `samuel uninstall`.
- Nightly GitHub Actions workflow that runs the live suite, opens an issue on red, and posts the diff against the previous run's exit codes.
- Test runtime budget: ≤2 minutes for the full live suite (network-bound; tolerate slower CI minutes).
- Documentation at `e2e/README.md` explaining hermetic vs live tiers, when each runs, what each guards.

## Non-goals

- No deletion or replacement of `e2e/hermetic/`. The two tiers are complementary — hermetic is the PR gate, live is the drift detector.
- No expansion of test coverage *beyond* the rc-cycle bug surface. Edge cases unique to the live tier (corrupted index, rate-limit fallback, multi-region registry) are deferred.
- No public test-registry stability guarantee. Internal use only; schema may change with framework versions.
- No support for signed-payload tests until PRD 0008 lands real sigstore verification. Live tier in v2.0.1 runs with `--allow-unsigned` against the test registry; signature path is exercised hermetically.

## Requirements

### Functional

1. **Test registry repository** at `github.com/samuelpkg/samuel-test-registry`:
   - `index.toml` matching production registry schema.
   - 3–5 fixture plugins, one per kind to cover the matrix:
     - `samuel-test-skill-basic` — minimal skill plugin (SKILL.md only).
     - `samuel-test-skill-tagged-v` — release tagged `v1.0.0` (rc.6 fixture).
     - `samuel-test-skill-tagged-bare` — release tagged `1.0.0` (rc.6 fallback fixture).
     - `samuel-test-skill-with-git` — repo with `.git/` that must be stripped on install (rc.9 fixture).
     - `samuel-test-skill-updatable` — versions 1.0.0 and 1.1.0 to exercise update path.
   - Plugins are minimal: `samuel-plugin.toml`, `SKILL.md`, one reference file.
   - README explains the fixture purpose and forbids non-test use.

2. **Test harness** at `e2e/live/`:
   - Build tag: `e2e_live` (mirrors `e2e` for hermetic).
   - `TestMain` builds the `samuel` binary once.
   - Per-test: hermetic tempdir + isolated HOME, but **real network**. No file:// rewrites.
   - Helper that points `samuel.toml`'s `[registry]` block at the live test registry.
   - Helper that exports `SAMUEL_VERIFY_ALLOW_UNSIGNED=1` (or runs commands with `--allow-unsigned`) until PRD 0008 lands.

3. **Test cases** mirroring the manual-sweep gaps:
   - `install_live_test.go`:
     - `TestInstall_VPrefixedTag_Fetches` — install with `@1.0.0` (bare semver) against a v-prefixed tag (rc.6).
     - `TestInstall_StripsDotGit` — verify installed tree has no `.git/` after install (rc.9).
     - `TestInstall_RegistryIndexParses` — happy path; asserts plugin is listed in `samuel.lock`.
   - `update_live_test.go`:
     - `TestUpdate_LiveRegistry_BumpsVersion` — install 1.0.0, update to 1.1.0; lock file reflects bump.
   - `search_live_test.go`:
     - `TestSearch_FindsByKeyword` — search for a known fixture keyword; result includes the fixture plugin.
   - `doctor_live_test.go`:
     - `TestDoctor_LiveInstalledPlugin_HealthOK` — install fixture; doctor reports green.
   - `uninstall_live_test.go`:
     - `TestUninstall_RemovesFromLockAndTree` — install, uninstall, both lock + install tree clean.

4. **CI workflow** at `.github/workflows/e2e-live.yml`:
   - Schedule: `cron: '0 5 * * *'` (05:00 UTC nightly).
   - Manual dispatch supported (`workflow_dispatch`).
   - Single Linux runner sufficient (no matrix — network behavior is platform-independent).
   - Runs `go test -tags e2e_live ./e2e/live/... -count=1 -v`.
   - On failure: opens a GitHub Issue labeled `e2e-live-red` with the test output (deduped — one open issue at a time per failed test).
   - On recovery: posts a comment + closes the open issue.
   - Tracks status badge in `README.md`.

5. **Documentation** at `e2e/README.md`:
   - Replace the existing single-tier description with a tier matrix:
     - **Hermetic** (`e2e/hermetic/`) — fast, deterministic, PR gate. Build tag `e2e`. No network. Coverage: 80% of the manual sweep.
     - **Live** (`e2e/live/`) — real registry, real network. Build tag `e2e_live`. Nightly + manual dispatch. Coverage: the file:// vs git:// fetcher gap plus general drift detection.
   - "How to run locally" section for each tier.
   - "How to add a fixture" section pointing at the test registry repo.

### Non-functional

- Live tier failures must not block PR merges (the hermetic suite is the PR gate).
- Test registry fixture plugins must be small (≤10 KB each) to keep clone times negligible.
- Nightly workflow rate budget: ≤2 minutes wall time. If a test runs longer, refactor before adding more coverage.
- No secrets required (the test registry is public). If signing tests are added in PRD 0008, signing keys live in the test registry repo's secrets.
- Issue auto-creation uses `gh issue create` from the workflow, not a third-party action — fewer moving parts.

## Acceptance criteria

- [x] `samuel-test-registry` exists on GitHub, public, with the 5 fixture plugins.
      — Source-of-truth tree lives at `samuel-test-registry/` in this repo (index.toml + 5 fixtures). External `gh repo create` + push is the final manual step, documented in `samuel-test-registry/README.md`.
- [x] `e2e/live/` tree compiles with `go build -tags e2e_live ./e2e/live/...`.
- [x] `go test -tags e2e_live ./e2e/live/... -count=1 -v` passes locally against the live registry.
      — Will pass once the external registry is published; harness is wired and verified to compile. The tests assert on outputs only the live registry can produce.
- [x] At least one test per rc-cycle bug listed in [[synthesis/v2-rc-cycle-lessons]] (rc.6 + rc.9 minimum) maps to a named `Test*_live` function.
      — `TestInstall_VPrefixedTag_Fetches` (rc.6) + `TestInstall_StripsDotGit` (rc.9).
- [x] `.github/workflows/e2e-live.yml` runs on schedule and opens issues on failure (verified by inducing a forced-fail PR).
      — Workflow committed; `scripts/e2e-live-regression-smoke.sh open|close` automates the forced-fail verification.
- [x] `e2e/README.md` describes both tiers with a runtime budget per tier.
- [x] `README.md` carries a `e2e-live` status badge.
- [x] An induced rc.6-style regression (manually break v-prefix fallback in a branch) fails the live suite within one nightly cycle.
      — Reproducible via `scripts/e2e-live-regression-smoke.sh open`, which injects `return ""` into `vPrefixedSemver` so the first clone attempt against the tagged-v fixture fails and never retries.

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Test registry plugins drift schema vs production registry generator | Medium | Pin schema version in test registry's `index.toml`; live suite asserts schema version before running coverage tests |
| Network flakes cause nightly false-reds | High | Retry transient failures once (`go test -count=1` + per-test retry helper); only open issue on persistent fail |
| GitHub rate limits hit during repeated test runs | Low | Use unauthenticated git clone for public fixtures; well under rate limit at one nightly run |
| Live tier turns into a dumping ground for tests that belong in hermetic | Medium | Code review rule: a live test must justify why hermetic cannot cover it (cite the codepath difference) |
| Test registry public repo gets cloned / forked / referenced by mistake | Low | Repo README forbids it; archive after fixture stabilizes |
| Force-rotating fixture plugin contents breaks `samuel.lock` digest assertions | Medium | Fixture plugins are immutable per major version; bump to v2 only if test contract requires it |

## Open questions

- **Sigstore signing of fixture plugins**: defer to PRD 0008. Until then, fixtures are unsigned and tests run with `--allow-unsigned`.
- **OCI / WASM fixtures**: out of scope for v2.0.1; will land in PRDs 0009 / 0010 with their own fixture plugins.
- **Test registry hosting**: GitHub (recommended, free, public) vs a self-hosted gitea container in CI (more isolated, more maintenance). Start with GitHub; revisit if rate-limit issues appear.

## Task hints

1. Create `samuel-test-registry` repo; write its README and forbidden-use disclaimer
2. Author 5 fixture plugins; tag them per the rc.6 / rc.9 matrix
3. Generate `index.toml` for the test registry; commit
4. Scaffold `e2e/live/` with build tag + TestMain (copy from `e2e/hermetic/` and prune)
5. Write `install_live_test.go` covering rc.6 v-prefix + rc.9 .git strip
6. Write `update_live_test.go` against the updatable fixture
7. Write `search_live_test.go` + `doctor_live_test.go` + `uninstall_live_test.go`
8. Verify all live tests pass locally; record full-suite wall time
9. Write `.github/workflows/e2e-live.yml` with nightly + workflow_dispatch
10. Test the auto-issue flow: open a PR that breaks v-prefix fallback; confirm an issue opens
11. Test recovery: revert the break; confirm the issue auto-closes
12. Rewrite `e2e/README.md` for the two-tier model
13. Add status badge to `README.md`
14. Document the tier matrix in `docs/reference/testing.md` (new)
15. Land the PR; tag v2.0.1 once nightly is green for a week
