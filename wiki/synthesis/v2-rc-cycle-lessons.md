---
title: v2 release-candidate cycle (rc.2 → rc.15) — bug pattern + lessons
type: synthesis
created: 2026-05-13
updated: 2026-05-13
sources: []
tags: [v2, v2-decision, release-cycle, manual-testing, e2e, lessons-learned]
---

# v2 release-candidate cycle (rc.2 → rc.15)

Samuel v2.0 shipped at rc.2 on 2026-05-12, advertised as "Public release." Within 24 hours, a single contributor running a manual-test sweep against a fresh `samuel init` fixture surfaced enough breakage to drive **13 more release candidates** (rc.3 → rc.15) before the project reached genuine ship-ready state. Every regression caught was the kind a real user would have hit on day 1.

This page documents the bug pattern, the architectural shifts forced by manual testing, and the structural changes (built-in fixture + automated e2e suite) that close the gap so the same drift doesn't recur.

## Bug-pattern taxonomy

Ten issues were filed against rc.2 over the cycle. Three patterns dominated:

### 1. Schema drift between framework and registry generator (1 issue)

**[Issue #2 ancestor: rc.3 fix](https://github.com/samuelpkg/samuel/issues/2)** — rc.2's registry parser expected `[plugin.<name>]` map-of-tables; the registry generator emitted `[[plugins]]` array-of-tables with inline `name` fields. The TOML parsed cleanly (unknown fields are allowed), `Index.Plugins` ended up empty, and every search/info/install/update returned no results — silently.

**Lesson**: when two repos coordinate via a serialized format, the schema MUST be tested end-to-end against the actual published format, not just round-tripped through the writer's own tests. Unit tests in `internal/plugin/registry/registry_test.go` used the writer's shape; they passed while the user-facing path was broken.

### 2. Cache-key gaps that made policy invisible (1 issue + adjacent half)

**[Issue #2](https://github.com/samuelpkg/samuel/issues/2) (rc.7)** — `verify.Cache` keyed on file digest only. First `install --allow-unsigned` cached `Reason: "--allow-unsigned"` against that digest, and every subsequent install/update of the same plugin — *with or without the flag* — replayed the cached decision. The flag was effectively sticky and the underlying signature policy was invisible from the CLI. Fix: cache key now `(digest, AllowUnsigned)` pair.

**Lesson**: a cache key must cover every input that affects the cached result. Build this into the cache module's invariant comment, not just the test cases.

### 3. Dispatch gaps when a new feature lands in only half the codepath (2 issues)

- **[Issue #8](https://github.com/samuelpkg/samuel/issues/8) (rc.14)** — rc.11 added `plugin:<name>` health checks to `samuel doctor`. The summary correctly reported `1 fixable` for a corrupted plugin, but `samuel doctor --fix` errored with `no plugin matches plugin:foo`. Why: `attemptFix` only knew about orchestrator (framework-tier) plugins; user-installed plugins live in `samuel.lock` and reach via the install service, which `attemptFix` never touched.

- **The post-fix re-check** had the matching gap (would render the line as ✗ even after a successful repair, because the re-check loop also queried only the orchestrator).

**Lesson**: when a new tier of state is added (here: `plugin:<name>` in addition to framework `<component>`), every codepath that dispatches on component identity must learn the new prefix. Search the codebase for the old dispatch pattern and verify each branch handles the new case.

## Architectural shifts forced by manual testing

### Claude carve-out from agent-agnostic core (rc.4)

**Pre-rc.4 design** (recorded in [[CLAUDE]]): "AGENTS.md primary, not CLAUDE.md. Tool-specific files are translator plugins." The agnostic-check CI gate forbade `CLAUDE.md` in framework code.

**Manual-test reality**: every other major coding assistant (Codex CLI, Aider, Gemini CLI, Cline, Cursor) reads `AGENTS.md` natively. Claude Code is the one outlier. Requiring every Samuel user — likely the majority Claude users today — to install a plugin just to obtain a `CLAUDE.md` mirror was friction without payoff.

**Decision**: scoped carve-out. The `AGENTS.md → CLAUDE.md` mirror moves into `internal/translator/claude/` as a built-in. Every richer translator surface (Cursor rules, Codex specifics, future tools) stays in plugins. The agnostic-check CI gate is narrowed: `CLAUDE.md` is now an allowed string in framework code; `.claude/`, `.cursor/`, `.codex/`, `Cursor`, `Codex CLI` remain forbidden.

**Why this is principled, not retreat**: the agnostic-core principle was about preventing framework code from coupling to specific tools' implementation details. The Claude translator does not couple to Claude — it writes a markdown file. The carve-out is for a specific user-friction case, not a slippery slope.

#v2-decision Tagged for the wiki: see [[CLAUDE]] section "Resolved scope decisions."

### Default-on semantics for built-in features (rc.5)

rc.4 shipped the Claude translator behind a `[translators.claude] enabled = true` config gate. rc.5 discovered that pre-rc.4-era `samuel.toml` files (no `translators` section) had the section absent, which my gate-check logic interpreted as disabled. Every project initialized before rc.4 silently stopped getting CLAUDE.md updates after upgrading.

**Lesson**: a built-in default-on feature's config field exists for explicit opt-out, not opt-in. Absent configuration MUST be treated as the default. This is encoded in `Config.ClaudeTranslatorEnabled()`:

```go
if c.Translators == nil || c.Translators.Claude == nil {
    return true  // default-on
}
return c.Translators.Claude.Enabled
```

#v2-decision

### Honest disclosure of the verifier stub (rc.10)

Investigation of Issue #2 surfaced that the production verifier is `StubVerifier` — policy is enforced (`identity_patterns`, `allow_unsigned_for`, `AllowUnsigned`), but no Sigstore math runs. The CLI faithfully prints `signature: verified (...)`, but a user reading "verified" would reasonably assume cryptographic verification.

**Decision**: `samuel doctor` prints a one-line **Advisories** section calling out the stub state. README updated. Signing docs updated. `verify.IsProduction()` is the single source of truth (returns `false` when `Default()` is `StubVerifier`; flips to `true` when v2.1 swaps in `sigstore-go`).

**Lesson**: when a planned feature ships unfinished, surface it where users naturally check (`samuel doctor`), not just in code comments. The half-honest output ("verified" without qualifier) was the actively-misleading state; the surfaced advisory is the honest state.

#v2-decision

## Structural changes that close the recurrence gap

### Manual-test fixture lives in-tree (rc.13 commit `a512cd4`)

`examples/tetris/` ships a clean `samuel init` state + a sample PRD with inline `### N.M Title` task headings. Six tracked files; everything regenerable is gitignored. The fixture's README documents the full test recipe. Any contributor can reproduce the rc.2 → rc.15 manual sweep with `cd examples/tetris && samuel doctor`.

### `samuel init` ships `.samuel/.gitignore` (rc.13)

Found by post-test cleanup: `.samuel/lock` was leaking into `git status` despite the repo-level gitignore. The deeper truth: every external samuel project had the same leak, fixed only by manual gitignore hand-editing. `samuel init` now writes `.samuel/.gitignore` covering `lock`, `run/`, `plugins/`, `builtins/` — out of the box.

### Hermetic e2e suite codifies the manual sweep (rc.15)

`e2e/hermetic/` carries 27 tests across 5 blocks mirroring every manual-test block from the cycle. Each test builds the real `samuel` binary in `TestMain`, runs it via `exec.Command` against a hermetic tempdir + isolated HOME + local file:// registry, asserts on stdout/files/exit. Full suite runs in ~3 seconds locally and in CI. **Every regression caught manually now has an automated test that runs on every PR.**

The hermetic tier has one honest limitation: `file://` URLs route through `source.fetchFile`, not `source.fetchGit`, so the rc.6 (v-prefix tag fallback) and rc.9 (.git strip) fixes can't be exercised at the CLI surface from this tier. Both fixes are protected by unit tests in `internal/plugin/source/source_test.go` against a real local git repo. End-to-end coverage of the git-fetcher gap landed in v2.0.1 as the `e2e/live/` tier (build tag `e2e_live`, nightly cadence, auto-issue on red) — [Issue #10](https://github.com/samuelpkg/samuel/issues/10) is closed.

## The 14 RCs in one table

| RC | Closed | One-line summary |
|---|---|---|
| rc.3 | [#2 ancestor] | Registry parser accepts `[[plugins]]` shape; live registry suddenly resolves |
| rc.4 | [arch shift] | Built-in Claude translator (carve-out from agnostic core) |
| rc.5 | [adjacent] | Translator default-on when `[translators.claude]` section is absent |
| rc.6 | [arch fix] | Git fetcher retries with `v` prefix on bare-semver refs |
| rc.7 | #2 | Verifier cache keys on `(digest, AllowUnsigned)`; update accepts install flags |
| rc.8 | #5 | `samuel run start` prints actionable hint on empty queue (was silent exit 0) |
| rc.9 | #1 | Plugin install strips cloned `.git/` from the install tree |
| rc.10 | #6 | `samuel doctor` advisory surfaces the StubVerifier disclosure |
| rc.11 | #3 | Doctor verifies installed plugins against `samuel.lock` |
| rc.12 | #4 | PRD parser accepts inline `### N.M Title` task headings |
| rc.13 | [hygiene] | `samuel init` writes `.samuel/.gitignore`; fixture lives in-tree |
| rc.14 | #8, #9 | Doctor `--fix` repairs plugins; install `--dry-run` is honest |
| rc.15 | #7 (partial) | Hermetic e2e test suite (27 tests, ~3s, CI-gated) |

## What's left after rc.15

- ~~**[Issue #10](https://github.com/samuelpkg/samuel/issues/10)** — `e2e/live/` tier.~~ **Closed in v2.0.1** by PRD 0007. Nightly drift detection against `samuel-test-registry` exercises the `source.fetchGit` codepath (rc.6 v-prefix fallback, rc.9 `.git` strip) plus install/update/search/doctor/uninstall at the CLI surface. Auto-issue on red, auto-close on recovery.
- **Remaining open after v2.0.1**: real Sigstore math via `sigstore-go` (PRD 0008, [Issue #6](https://github.com/samuelpkg/samuel/issues/6) partial) for v2.1. The wire format + lockfile schema stay stable across that transition; the live e2e tier currently runs with `SAMUEL_VERIFY_ALLOW_UNSIGNED=1` until signing lands.
- **WASM and OCI plugin tiers** — both depend on at least one published plugin of each kind existing in the live registry. Out of scope for v2.0.

## Tagged entities introduced this cycle

- `examples/tetris` — in-tree manual-test fixture #v2-decision
- `e2e/hermetic` — hermetic e2e test suite #v2-decision
- `internal/translator/claude` — built-in Claude translator #v2-decision
- `verify.StubVerifier` — v2.0 policy-only verifier #rescue (placeholder for sigstore-go in v2.1)
- `Config.ClaudeTranslatorEnabled()` — default-on helper that codifies the "absent config = default" pattern

---

Created: 2026-05-13
