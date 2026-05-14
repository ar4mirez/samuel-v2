# Tasks — PRD 0007: Live-Registry E2E Tier

> Generated from [0007-prd-live-registry-e2e.md](0007-prd-live-registry-e2e.md) on 2026-05-13.
> Depends on PRD 0006 (Polish + Launch) being complete.
> Target release: v2.0.1.

## Relevant files

- `e2e/hermetic/` — existing hermetic tier; pattern source for the new tier
- `e2e/README.md` — to be rewritten for two-tier model
- `internal/plugin/source/source.go` — `fetchGit` codepath the live tier must exercise
- `internal/plugin/source/source_test.go` — existing unit tests for rc.6 / rc.9 fixes (live tier extends, doesn't replace)
- `internal/plugin/registry/` — registry parser the live tier exercises against a real index.toml
- `samuel-test-registry/` — NEW external repo at `github.com/samuelpkg/samuel-test-registry`
- `e2e/live/` — NEW test tree, build tag `e2e_live`
- `.github/workflows/e2e-live.yml` — NEW nightly workflow
- `README.md` — status badge addition
- `docs/reference/testing.md` — NEW tier-matrix reference

## Tasks

- [ ] 1.0 Test registry repo + fixture plugins [~4,000 tokens - Medium]
  - [ ] 1.1 Create `github.com/samuelpkg/samuel-test-registry` (public, README forbids non-test use)
  - [ ] 1.2 Author fixture `samuel-test-skill-basic` (minimal SKILL.md + manifest); tag `v1.0.0`
  - [ ] 1.3 Author fixture `samuel-test-skill-tagged-v` (release tagged `v1.0.0` — rc.6 fixture)
  - [ ] 1.4 Author fixture `samuel-test-skill-tagged-bare` (release tagged `1.0.0` — rc.6 fallback fixture)
  - [ ] 1.5 Author fixture `samuel-test-skill-with-git` (repo with `.git/` left after clone — rc.9 fixture)
  - [ ] 1.6 Author fixture `samuel-test-skill-updatable` (versions `1.0.0` and `1.1.0` for update path)
  - [ ] 1.7 Generate `index.toml` matching production registry schema; commit
  - [ ] 1.8 Verify each fixture clones + parses via local `samuel install` against the live URL

- [ ] 2.0 e2e/live/ harness scaffold [~2,500 tokens - Simple]
  - [ ] 2.1 Create `e2e/live/` tree; copy `TestMain` skeleton from `e2e/hermetic/main_test.go`
  - [ ] 2.2 Add build tag `e2e_live` (separate from hermetic's `e2e`)
  - [ ] 2.3 Drop file:// rewrites; tests use real network against `github.com/samuelpkg/samuel-test-registry`
  - [ ] 2.4 Helper: `withLiveRegistry(t, configureFn)` materializes a tempdir + isolated HOME + `samuel.toml` pointing at the live registry
  - [ ] 2.5 Helper: `withAllowUnsigned(t)` exports `SAMUEL_VERIFY_ALLOW_UNSIGNED=1` until PRD 0008 lands real signing
  - [ ] 2.6 `go build -tags e2e_live ./e2e/live/...` compiles clean

- [ ] 3.0 Install test cases (rc.6 + rc.9 + happy path) [~3,000 tokens - Medium]
  - [ ] 3.1 `install_live_test.go`: `TestInstall_VPrefixedTag_Fetches` — install with `@1.0.0` resolves against `v1.0.0` tag (rc.6 protection)
  - [ ] 3.2 `TestInstall_StripsDotGit` — install fixture-with-git; assert `~/.samuel/plugins/<name>/.git/` is absent (rc.9 protection)
  - [ ] 3.3 `TestInstall_RegistryIndexParses` — happy path; assert plugin in `samuel.lock` with correct version + digest
  - [ ] 3.4 `TestInstall_UnknownPlugin_StructuredError` — install nonexistent name; assert structured error with DocsURL

- [ ] 4.0 Update / search / doctor / uninstall test cases [~3,000 tokens - Medium]
  - [ ] 4.1 `update_live_test.go`: `TestUpdate_LiveRegistry_BumpsVersion` — install 1.0.0 → update to 1.1.0; lock reflects bump
  - [ ] 4.2 `search_live_test.go`: `TestSearch_FindsByKeyword` — known-keyword search returns the fixture plugin
  - [ ] 4.3 `doctor_live_test.go`: `TestDoctor_LiveInstalledPlugin_HealthOK` — install fixture; `samuel doctor --json` reports green
  - [ ] 4.4 `uninstall_live_test.go`: `TestUninstall_RemovesFromLockAndTree` — install, uninstall; both lock + plugin tree clean

- [ ] 5.0 Wall-time + flake budget [~1,500 tokens - Simple]
  - [ ] 5.1 Record full-suite wall time on reference laptop; fail if >2 min
  - [ ] 5.2 Per-test retry helper (`retryOnce(t, fn)`) for transient network failures; cap at 1 retry
  - [ ] 5.3 Document the "live tests can fail; PR gate is hermetic" contract in `e2e/live/README.md`

- [ ] 6.0 Nightly CI workflow + auto-issue [~3,500 tokens - Medium]
  - [ ] 6.1 Author `.github/workflows/e2e-live.yml` with cron `0 5 * * *` + `workflow_dispatch`
  - [ ] 6.2 Single Linux runner; runs `go test -tags e2e_live ./e2e/live/... -count=1 -v`
  - [ ] 6.3 On failure: `gh issue create` with deduped title `[e2e-live] <test name> failing`; labels `e2e-live-red`, `nightly`
  - [ ] 6.4 On recovery: `gh issue comment` + `gh issue close` for matching open issues
  - [ ] 6.5 Status badge in `README.md` linking to the workflow page
  - [ ] 6.6 Smoke-test the auto-issue flow: induce a forced-fail PR, verify issue opens; revert, verify issue auto-closes

- [ ] 7.0 Documentation refresh [~2,500 tokens - Simple]
  - [ ] 7.1 Rewrite `e2e/README.md` for the two-tier matrix (hermetic vs live; when each runs; what each guards)
  - [ ] 7.2 Add "How to run locally" section per tier
  - [ ] 7.3 Add "How to add a fixture" section pointing at the test registry
  - [ ] 7.4 New `docs/reference/testing.md` documenting the tier matrix at the user-facing level
  - [ ] 7.5 Add `e2e-live` status badge to top of `README.md`

- [ ] 8.0 Validate against a real regression [~1,500 tokens - Simple]
  - [ ] 8.1 In a throwaway branch, intentionally break v-prefix fallback in `source.fetchGit`
  - [ ] 8.2 Run nightly workflow manually; verify `TestInstall_VPrefixedTag_Fetches` fails
  - [ ] 8.3 Verify auto-issue opens with the failing test name
  - [ ] 8.4 Revert; verify nightly green; verify issue auto-closes

- [ ] 9.0 Release v2.0.1 [~1,500 tokens - Simple]
  - [ ] 9.1 CHANGELOG `## [v2.0.1]` entry: lede = live e2e tier closes rc.6/rc.9 surface gap
  - [ ] 9.2 Tag `v2.0.1-rc.1`; goreleaser publishes signed artifacts
  - [ ] 9.3 After 1 week green nightly: tag `v2.0.1`
  - [ ] 9.4 Update `wiki/synthesis/v2-rc-cycle-lessons.md` — mark Issue #10 closed; note remaining gap (sigstore math = PRD 0008)
