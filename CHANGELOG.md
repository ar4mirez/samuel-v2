# Changelog

All notable changes to Samuel v2 are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
this project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v2.0.0-rc.9] — Drop plugin repo `.git/` from installs

Closes [Issue #1](https://github.com/samuelpkg/samuel/issues/1).

### Fixed

- **`samuel install` no longer copies the plugin repo's `.git/`
  directory into `.samuel/plugins/<name>/`.** Samuel resolves
  installed plugins by name + lockfile digest, not by walking commit
  history, so the git plumbing had no downstream consumer. It
  inflated installs (`actix-web` dropped from 31 files / 176K to
  6 files / 56K — a 68% reduction), surprised IDE git integrations
  with nested repos, and made `find -name .git` noisy across the
  project tree. `fetchGit` now removes the cloned `.git/` directory
  after a successful clone, before returning the materialized source
  to the install pipeline.

## [v2.0.0-rc.8] — run start: actionable empty-queue exit

Closes [Issue #5](https://github.com/samuelpkg/samuel/issues/5).

### Fixed

- **`samuel run start` no longer exits silently when the loop has no
  pending tasks.** The ralph loop's empty-queue branch
  ([loop.go:135-139](internal/methodology/ralph/loop.go#L135-L139))
  cleanly `break`s, but it returned that signal nowhere — so the CLI
  exited 0 with zero output, indistinguishable from a successful run
  or a crash in CI logs. `runRunStart` now detects empty-queue +
  non-pilot before invoking the loop and emits:
  - text mode: `→ No pending tasks. Add one with samuel run enqueue <title>, or initialize from a PRD with samuel run init --prd <path>.`
  - JSON mode: standard envelope with `iterations_run: 0`, `pending_tasks: 0`, `message: "no pending tasks; nothing to do"`.

  Pilot mode is exempt — its whole job is to discover tasks from an
  empty queue.

## [v2.0.0-rc.7] — Verify cache key + update signature flags

Closes [Issue #2](https://github.com/samuelpkg/samuel/issues/2).

### Fixed

- **Verifier cache now keys on `(digest, AllowUnsigned)`, not digest
  alone.** Previously, the first `samuel install <name> --allow-unsigned`
  cached `Reason: "--allow-unsigned"` against the plugin file's digest,
  and every subsequent install/update of the same plugin — *with or
  without the flag* — replayed that cached decision. The flag was
  effectively sticky and the underlying signature policy was invisible
  from the CLI. The cache now stores `(digest, AllowUnsigned)` pairs
  separately, so toggling the flag re-runs the policy check.
- **`samuel update <name>` now accepts the same signature/policy flags
  as `samuel install`**: `--allow-unsigned`, `--allow-prerelease`,
  `--non-interactive`, `--dry-run`. Previously these were rejected as
  unknown flags, leaving update users no way to control trust at
  update time.
- **`samuel update <name>` now reports the verification reason** in
  its success line — `actix-web -> 1.0.0 (verified (github.com/...))`
  — matching the install command's signature line. Previously the
  output read only `actix-web -> 1.0.0` and gave no signal about
  whether the install path's signature policy actually ran.

## [v2.0.0-rc.6] — Plugin install fetcher: v-prefix tag fallback

### Fixed

- **`samuel install <plugin>` now resolves `vX.Y.Z` repo tags from
  bare-semver registry refs.** Every plugin in the official registry
  publishes `latest = "1.0.0"` (no v prefix), but the corresponding
  plugin repos tag releases as `v1.0.0` (Go / goreleaser convention).
  rc.5's fetcher passed the ref verbatim to `git clone --branch`, so
  every install attempt failed with `fatal: Remote branch 1.0.0 not
  found in upstream origin` — the registry → install path was
  end-to-end broken against the real plugin ecosystem.

  The fetcher now retries the clone with a `v` prefix when (a) the ref
  looks like a bare semver and (b) the first attempt failed
  specifically with a missing-ref error. Other failure modes (network,
  auth, permissions) surface immediately so they aren't masked by a
  spurious retry. New unit tests cover both the fallback path and the
  exact-ref path.

## [v2.0.0-rc.5] — Translator default-on fix

### Fixed

- **Built-in Claude translator now runs by default on existing
  projects.** rc.4 required the `[translators.claude]` section to be
  present in `samuel.toml` for the mirror to fire, so every project
  initialized before rc.4 silently stopped getting CLAUDE.md updates
  after upgrading. The new `Config.ClaudeTranslatorEnabled()` helper
  treats absent configuration as enabled (the section is for explicit
  opt-out only). Both `samuel init` and `samuel sync` go through the
  helper.

## [v2.0.0-rc.4] — Claude translator carve-out

Built-in AGENTS.md → CLAUDE.md mirror lands in core. Agent-agnostic
core is preserved with a deliberate, scoped exception for Claude — the
only major coding assistant that does not read AGENTS.md natively.

### Added

- Built-in **Claude translator** (`internal/translator/claude/`) that
  mirrors AGENTS.md → CLAUDE.md on every `samuel init` and `samuel sync`.
  Default-on; controlled by `[translators.claude] enabled = true` in
  `samuel.toml`. Files without the translator's autogen marker
  (v1-authored or hand-edited CLAUDE.md) are preserved unless
  `--force` is set.

### Changed

- **Agent-agnostic architecture: deliberate, scoped Claude carve-out.**
  Claude Code is the only major coding assistant that does not read
  AGENTS.md natively. Requiring every Samuel user to install a plugin
  for the trivial mirror was friction without payoff, so the mirror is
  now built in. Every richer translator surface (per-folder rule files,
  glob matchers, agent prompts, future tools) still belongs in plugins.
  The agnostic-check CI gate has been narrowed accordingly: `CLAUDE.md`
  may appear in framework code; `.claude/`, `.cursor/`, `.codex/`, and
  the literal "Cursor" / "Codex CLI" tokens remain forbidden.
- `samuel doctor` no longer suggests installing `claude-translator`
  (built in) or `codex-translator` (Codex reads AGENTS.md natively).
  The `cursor-translator` suggestion remains because Cursor's rule
  surface is richer than a flat mirror.
- `samuel init` adds a "mirror AGENTS.md → CLAUDE.md for Claude Code"
  line to the pre-init plan, and the success summary reports the
  translator's create/update/skip counts.
- v1 migration warning rephrased: pre-existing CLAUDE.md is still left
  untouched, but the message now points users at the opt-in path
  (delete + `samuel sync`).

## [v2.0.0] — Public release

PRD: [0006-prd-polish-launch.md](.samuel/tasks/0006-prd-polish-launch.md)

**Thesis.** v2.0 is a clean break from v1. The framework is small, agent-agnostic,
and plugin-driven. AGENTS.md is the canonical context file; everything tool-specific
(CLAUDE.md, Cursor rules, Codex files) is produced by a translator plugin. The
auto-mode loop is preserved under the methodology hooks model — Ralph Wiggum
remains the default, now with extension points. Plugins ship in three tiers
(skill / WASM / OCI), each with a Sigstore-keyless signature and a TOON manifest
that declares capabilities up-front.

What survived from v1: the 4D methodology, the autonomous iteration loop, the
structured-error UX (`Fix:` / `DocsURL:`), the lock semantics, the JSON envelope.
What is gone: gstack composition, gbrain MCP registration, language / framework /
workflow as separate enums in the schema (now all plugins). What is new: three-tier
plugin loader, capability model with safe-default vs risky split, methodology hooks,
TOON-encoded runtime, CLI-mutation pattern for state, translator plugins.

### Added

- **Charm UI** across `internal/ui/`: `lipgloss` (color/style),
  `huh` (native multi-select + confirm prompts, replacing the v1 inline
  scanln path), `bubbles/spinner` (between-iteration indicator).
  Same six-category vocabulary (success/error/warn/info/bold/dim);
  same JSON envelope (schemaVersion 4); callers untouched.
  Rationale: huh ships the native multi-select v1 hand-rolled, and the
  lipgloss adaptive-color model erases the v1 light/dark hand-tuning.
- **`docs/` site** at `samuelpkg.github.io/samuel/`, restructured per
  RFD 0001 / RFD 0002: drops the `languages/`, `frameworks/`, and
  `workflows/` SKILL.md duplicates from v1; adds `concepts/`,
  `plugin-authors/`, and an auto-generated `plugins/` index pulled
  from `samuelpkg/samuel-registry`. Material theme, `mkdocs --strict`
  clean.
- **Eight inaugural RFDs** at `docs/rfd/0001.md` through `0008.md`:
  three-tier plugin architecture, AGENTS.md primary,
  SemVer/capability/Sigstore, methodology hooks, component lifecycle as
  plugin loader, `samuel run [methodology]` rename, plugin migration
  from v1 skills, drop gstack/gbrain. `rfd-index.toml` is the source of truth;
  `scripts/gen-rfd-index.sh` regenerates `docs/rfd/index.md`.
- **Migration notice** at `docs/getting-started/migration-v1.md`.
  Not an upgrade tool — explains the clean break, the `v1-final` tag,
  the binary-name collision (v2 overwrites v1), how to start fresh.
- **CI gate `agents-md-check.yml`**: enforces the ≤150-line budget on
  both `template/AGENTS.md.tmpl` source and on the rendered max-config
  output (mirrored by `TestAgentsMDTemplate_*` in the template
  package).
- **CI gate `agnostic-check.yml`**: greps `internal/`, `cmd/`,
  `template/` for `CLAUDE.md`, `.claude/`, `.cursor/`, `.codex/`,
  `Cursor`, `Codex CLI`. Passes today; fails any PR that re-introduces
  tool-specific coupling outside a translator plugin. Test files and
  `agnostic-allow` opt-outs are exempt.
- **CHANGELOG** at repo root with a v1-style entry (rationale per item,
  internal section, deprecation notice). README links to it.
- **`scripts/gen-plugins-pages.sh`**: docs-build-time generator that
  fetches `samuel-registry/index.toml` and writes one page per plugin
  under `docs/plugins/`.

### Changed

- **AGENTS.md template** trimmed to 104 source lines (rendered ≤150
  under the saturated test fixture). Sections: 4D methodology,
  boundaries, quick reference, plugin block, guardrails block,
  quality checks, project context.
- **`samuel install`** consent flow now uses `huh.Confirm` when stdin
  is a TTY and falls back to scanln when piped (CI / scripted use).
  Behavior unchanged; UX is the v1 multi-select replacement.

### Deprecated

None. v2.0 is the clean break; deprecations apply only to v1.

### Removed

- **gstack composition** (was a v1 plugin-composition mechanism).
  Rationale: replaced by the simpler three-tier plugin model and
  meta plugins (`samuel-starter`). RFD 0008.
- **gbrain MCP registration** path. Rationale: the framework no
  longer registers MCP servers on the user's behalf; plugins that
  need MCP do it explicitly.
- **`languages` / `frameworks` / `workflows`** dirs from the docs
  site (they were SKILL.md duplicates). Per-plugin pages are now
  generated from the registry index.

### Internal

- `internal/ui/prompts.go` — new file. `Confirm`, `Select`,
  `MultiSelect` over `huh`; `IsTerminal()` gate; scanln fallback.
- `internal/ui/output.go` — `lipgloss` adaptive-color tokens; same
  helper surface (`Success`, `Error`, `Warn`, `Info`, `Bold`, `Dim`,
  `Header`, `Section`, `ListItem`, `SuccessItem`, `WarnItem`,
  `ErrorItem`, `TableRow`).
- `internal/ui/spinner.go` — bubbles/spinner wrapper for
  non-bubbletea callers; safe `Start`/`Stop`/`Success`/`Error`.
- `internal/commands/plugins.go` — `consolePrompt` rewritten to use
  `ui.Confirm`. Test injection via `testPrompt` preserved.
- `template/template_test.go` — two budget tests:
  `TestAgentsMDTemplate_LineBudget` (source) and
  `TestAgentsMDTemplate_RendersUnderBudget` (rendered against a
  saturated max-config fixture). `TestAgentsMDTemplate_RendersAllVisibleSections`
  asserts the 7 expected H2 sections.
- `.github/workflows/agents-md-check.yml`, `.github/workflows/agnostic-check.yml` —
  the two new gates. CI on push + PR for `template/**`, `internal/**`,
  `cmd/**`, `.github/workflows/**`.
- `scripts/gen-rfd-index.sh`, `scripts/gen-plugins-pages.sh` — both
  bash, both idempotent (`set -euo pipefail`, stable diffs on re-run).
- `docs/` — 29 new authored pages (~12,900 words). `mkdocs.yml` at
  repo root, Material theme.

### Migration

There is no in-place upgrade from v1. The binary name is the same;
installing v2 overwrites v1. The v1 source is preserved at the
`v1-final` tag in `github.com/samuelpkg/samuel`. The migration notice
in `docs/getting-started/migration-v1.md` covers what changes for v1
users (AGENTS.md replaces CLAUDE.md; install the
`samuel-claude-translator` plugin to keep CLAUDE.md generated; SKILL.md
files have been ported to plugins under `github.com/samuelpkg/samuel-<name>`;
no plugin authoring CLI yet — that ships in v2.1).

## [v2.0.0-rc.3] — Final soak

Final fixes from rc.2 feedback. CHANGELOG cleanup. No new features.

### Fixed

- Registry parser now accepts both the legacy `[plugin.<name>]` map shape
  and the array-of-tables `[[plugins]]` shape that the official registry
  generator emits. rc.2 only understood the former, so every
  `samuel search`, `samuel info`, `samuel install`, and `samuel update <name>`
  against the live registry returned `plugin not found` even though the
  index was fetched and cached correctly. `Index.Plugins` remains
  `map[string]Plugin` so no caller had to change; the new format is
  normalized inside `parseIndex`. Regression tests added for both shapes
  and a mixed-shape file.

## [v2.0.0-rc.2] — Polish + Launch (Milestone 6)

PRD: [0006-prd-polish-launch.md](.samuel/tasks/0006-prd-polish-launch.md)

First release candidate of v2.0. Charm UI swap complete; AGENTS.md
template + line-budget gate locked in; agnostic-by-design CI gate
added; eight inaugural RFDs published; docs site restructured.
goreleaser publishes signed artifacts.

### Added

- Charm UI swap (lipgloss + huh + bubbles).
- `agents-md-check.yml` CI gate enforcing the 150-line budget on both
  source and rendered output.
- `agnostic-check.yml` CI gate forbidding tool-specific references
  in `internal/`, `cmd/`, `template/`.
- Eight inaugural RFDs at `docs/rfd/0001.md` through `0008.md`.
- Docs site restructure under `docs/`, with `mkdocs.yml` at repo
  root. `concepts/`, `plugin-authors/`, `plugins/` (generated) new.
- `scripts/gen-rfd-index.sh` and `scripts/gen-plugins-pages.sh`
  generator scripts.
- Migration notice at `docs/getting-started/migration-v1.md`.

### Notes

- Feature freeze. Bug fixes only between rc.2 and v2.0.0.
- Internal-test target: a 1-week rc.2 → rc.3 soak, then a 1-week
  rc.3 → v2.0 soak. Defer any non-fix to v2.1.

## [v2.0.0-rc.1] — Skill Migration (Milestone 5)

PRD: [0005-prd-skill-migration.md](.samuel/tasks/0005-prd-skill-migration.md)

### Added

- `scripts/migrate-v1-skills`: one-shot Go tool that ports
  `samuel_v1/.claude/skills/*` into per-plugin source trees rooted at
  `migration-output/samuel-<name>/`. Parses v1 SKILL.md frontmatter
  (`metadata.category`, `metadata.language`, `metadata.extensions`,
  `metadata.source`), generates a valid `samuel-plugin.toml` per skill,
  copies `scripts/` `references/` `assets/` unchanged, and emits a
  README, MIT LICENSE, and reusable-workflow release.yml stub for each.
  `-dry-run` prints planned operations; the script is idempotent.
  Excludes `initialize-project` + `update-framework` (no v2 analogue)
  and the 7 `author: anthropic` skills (registered as upstream subpath
  entries in the registry instead of being ported).
- [`samuelpkg/samuel-plugin-release`](https://github.com/samuelpkg/samuel-plugin-release): reusable GitHub Actions workflow
  consumed by every plugin repo. Dispatches on `kind` — `skill` → tar.gz
  blob, `wasm` → TinyGo build, `oci` → docker buildx + GHCR push. All
  artifacts cosign-signed via keyless OIDC. Plugin repos pin
  `uses: samuelpkg/samuel-plugin-release/.github/workflows/release.yml@v1`.
- [`samuelpkg/samuel-registry`](https://github.com/samuelpkg/samuel-registry): registry repo — `index.toml`
  with 77 plugin entries (70 ported + 7 anthropic upstream), README and
  CONTRIBUTING, and a `validate` workflow that schema-checks the index,
  HEAD-checks every repo URL, and confirms each `latest` tag exists.
- [`samuelpkg/samuel-starter`](https://github.com/samuelpkg/samuel-starter): meta-plugin (kind = `meta`) whose
  `[requires]` declares the 12 Samuel-Way workflow plugins. Installed
  by default on `samuel init`; opt-out via `--minimal` or
  `--without <name>,<name>`.
- [`samuelpkg/samuel-claude-translator`](https://github.com/samuelpkg/samuel-claude-translator): TinyGo source for the
  WASM-tier translator plugin. Hooks `init.after` (writes
  `.claude/settings.json` with PreToolUse stubs) and `sync.after`
  (mirrors AGENTS.md → CLAUDE.md). Capabilities scoped to
  `/workspace/**/CLAUDE.md` and `/workspace/.claude/**`.
- [`samuelpkg/samuel-codex-translator`](https://github.com/samuelpkg/samuel-codex-translator): TinyGo source for the symmetric
  Codex-flavored translator. Hooks `sync.after` and emits
  `.codex/<rel>/context.md` per AGENTS.md.
- `internal/plugin/manifest`: `KindMeta` is now a recognized plugin
  kind. The validator requires meta plugins to declare a non-empty
  `[requires]` block.
- `internal/commands/plugin_admin`: `samuel plugin validate` and
  `samuel plugin info` subcommands consumed by the reusable release
  workflow (`samuel plugin validate samuel-plugin.toml`,
  `samuel plugin validate --registry index.toml`,
  `samuel plugin info --kind`, `--name`, `--version`).
- `scripts/smoke-test.sh`: end-to-end acceptance script covering every
  invariant in PRD 0005 §Acceptance — clean `samuel init`, language +
  framework skill installs, both translator plugins, agnostic
  invariant (no CLAUDE.md pre-translator-install), `--minimal`, and
  `--without`. `--offline` mode skips registry-dependent steps.
- `scripts/push-plugin-repo.sh`: deploy-side helper that takes one
  generated tree and gets it onto GitHub (git init when missing,
  always `git add -A`, commit only on drift, tag once, `gh repo
  create` if absent, `git push`). Fail-loud — earlier drafts masked
  the exit code through a pipe and silently dropped 52 of 70 plugins'
  `.github/` and `references/` directories.
- `scripts/patch-plugin-workflow.sh` + `scripts/ensure-plugin-workflow.sh`:
  Contents-API helpers to repoint or create `release.yml` across many
  plugin repos without cloning each one.
- `scripts/sync-plugin-content.sh`: reconciliation helper that mirrors
  the canonical generated tree on top of an existing samuelpkg/* repo,
  used to recover the dropped content from the initial bulk push.

### Notes

- The 70 ported plugin repos live at `github.com/samuelpkg/samuel-<name>`.
  They are not committed to this monorepo; regenerate locally on demand
  under `migration-output/` (gitignored).
- The reusable release workflow and auxiliary repos are at
  `github.com/samuelpkg/{samuel-plugin-release,samuel-registry,samuel-starter,samuel-claude-translator,samuel-codex-translator}`.
  Originally pushed under `ar4mirez/` and transferred to `samuelpkg/`
  for org consolidation. GitHub redirects keep the old URLs working
  until the new ones propagate everywhere.

## [v2.0.0-beta.2] — Methodology (Milestone 4)

PRD: [0004-prd-methodology.md](.samuel/tasks/0004-prd-methodology.md)

### Added

- `internal/methodology/hooks`: lifecycle-hook framework with the 13
  hook points from RFD 0004 (`before:loop`, `after:loop`,
  `before:iteration`, `after:iteration`, `iteration.gate`,
  `context.{snapshot,progress,task,extra}`,
  `before:agent.invoke`, `agent.invoke`, `after:agent.invoke`,
  `quality.check`). `Registry` resolves handler order from source
  (user override → built-in default → plugin) and honours per-hook
  `strict`, `timeout`, and `OrderOverride` from `samuel.toml`.
  Plugin handlers go through a `CapabilityChecker` before invocation.
- `internal/methodology/ralph/prd`: v2 port of `AutoPRD`,
  `AutoProject`, `AutoConfig`, `PilotConfig`, `AutoTask`,
  `AutoProgress`. Schema version `2.0`. Encodes to and decodes from
  TOON via tabular `tasks[N]{...}:` rows; AI-emitted numeric IDs are
  coerced to strings on load.
- `internal/methodology/ralph/prd.PRD I/O`: `Load`, `Save`,
  atomic write-tmp-then-rename. Validate catches duplicate IDs,
  missing titles, invalid statuses, dependency cycles.
- `internal/methodology/ralph/context`: pre-computed context
  generators ported from v1 — `GenerateProjectSnapshot` (TOON),
  `GenerateProgressContext` (Markdown), `GenerateTaskContext`
  (TOON, impl vs discovery shape), `RotateProgressIfNeeded`
  (500-line default threshold).
- `internal/methodology/ralph`: `RunAutoLoop` driver with per-iteration
  prd.toon reload, hook firing at the 13 lifecycle points,
  `MaxConsecFails` abort, `PauseSecs` pacing, `--profile` timings,
  `--dry-run` short-circuit.
- `internal/methodology/ralph.RegisterDefaults`: built-in handlers for
  `context.{snapshot,progress,task}`, `iteration.gate`,
  `quality.check`, `before:loop`.
- `internal/methodology/ralph/pilot`: `NewPilotConfig`,
  `ShouldRunDiscovery` (empty queue / interval / preemptive trigger),
  `InitPilotPRD` with focus-area injection.
- `internal/agents`: `AgentAdapter` interface +
  five built-in adapters (`claude`, `codex`, `copilot`, `gemini`,
  `kiro`). Each declares prompt-mode (content-arg, file-arg, stdin),
  env allowlist, default image, default args.
- `internal/sandbox`: `Runner` implements `agents.CommandRunner` with
  `none` (host exec), `oci` (container via the Milestone 3 OCI tier
  loader), and `dry-run` modes. OCI mount layout: `/workspace` rw,
  `/skills` ro, `/.samuel/run` ro (CLI-mutation invariant),
  `/plugin/config` ro, `/samuel-bridge`. Env allowlist filter and
  user mapping (`--user UID:GID`) preserved.
- `internal/methodology/ralph/templates`: embedded prompt templates
  (`prompt.md.tmpl` + `discovery-prompt.md.tmpl`) using `text/template`
  with helpers (`join`, `indent`, `relpath`, `hasPlugin`,
  `focusDescription`). Per-project override at
  `.samuel/templates/ralph/*.md.tmpl` shadows the embedded default.
  Prompts now instruct the agent to use `samuel run done|skip|reset|enqueue`
  CLI subcommands — they never edit `prd.toon` directly.
- `internal/commands/run.go` + `internal/commands/run_mutations.go`:
  `samuel run [methodology]` command surface — positional methodology
  argument with alias map (`rw` → `ralph`), smart bare invocation,
  `init`, `start`, `status`, `pilot`, `convert`, `tasks`, `done`,
  `skip`, `reset`, `enqueue`, `task add`. All mutations acquire the
  per-project file lock and persist atomically. `samuel auto` is a
  permanent alias (v1 compat).
- Tests: hook composition with two plugins + default chain, strict-mode
  abort, non-strict warning + continue, capability-deny path, timeout,
  agent-swap from claude to codex, per-project template override,
  TOON per-row malformation recovery, agnostic invariant (no
  `.claude/` paths written).

### Changed

- Runtime files now live at `.samuel/run/` (TOON for structured,
  Markdown for journals): `prd.toon`, `task-context.toon`,
  `project-snapshot.toon`, `progress.md`, `progress-context.md`.

## [v2.0.0-beta.1] — Plugin Loader (Milestone 3)

PRD: [0003-prd-plugin-loader.md](.samuel/tasks/0003-prd-plugin-loader.md)

### Added

- `internal/plugin/manifest`: `samuel-plugin.toml` parser + validator
  with structured `*errors.Error` (`SAM-MANIFEST-001`). Schema covers
  `[samuel]` framework/protocol ranges, `[provides]`, `[requires]`,
  `[capabilities]`, `[metadata]`, plus tier-specific `[wasm]` /
  `[oci]` blocks per RFD 0003.
- `internal/plugin/semver`: hand-rolled SemVer 2.0 + Cargo range
  resolver (`^X.Y.Z`, `~X.Y.Z`, `>=X,<Y`, `*`, exact). Prereleases
  rejected unless the resolver is called with `AllowPrerelease`.
- `internal/plugin/capability`: capability namespace
  (`filesystem.read/write`, `exec`, `network.outbound`, `samuel.api`,
  `assistant.invoke`); safe-default skip rule (`filesystem.read:/workspace`-
  only never prompts), `--yes` flag short-circuit, doublestar-backed
  path-glob matching, host pattern matching for outbound allowlists.
- `internal/plugin/registry`: `index.toml` schema parser, multi-source
  first-match-wins resolver, on-disk cache at
  `~/.samuel/cache/registries/<host>/<path>/index.toml` with 24h TTL
  and stale-cache fallback when the network is down. Supports
  `github.com/owner/repo`, raw HTTPS, and `file://` sources.
- `internal/plugin/verify`: signature-policy gate (`Verifier` interface +
  `StubVerifier` for v2.0). Cache at `~/.samuel/cache/verify/` keyed
  by samuel binary version. Identity patterns OR-ed per RFD 0003 #3.
  `--allow-unsigned` flag override; `[security]` block from
  `samuel.toml` plus `allow_unsigned_for` registry allowlist.
- `internal/plugin/source`: `Fetcher` abstraction with three transports
  (`file://`, `https://`, `github.com/owner/repo` shorthand). `git
  clone --depth=1 --branch=<ref>` for production; file:// for tests.
- `internal/plugin/skill`: skill-tier loader. Atomic `tmp →
  rename` install of `SKILL.md` + assets into
  `<project>/.samuel/plugins/<name>/`, frontmatter-shape validation
  on Check, idempotent uninstall.
- `internal/plugin/wasm`: wazero-backed WASM-tier loader. Embedded
  pure-Go runtime; per-process compilation cache at
  `~/.samuel/cache/wasm-compiled`; host module `samuel.*` exposing
  `log`, `fs_read`, `fs_write`, `exec`, `net_outbound`, `config_get`,
  `callback`, each capability-gated through `HostState.Authorize`.
  Module protocol enforced via the `samuel_protocol_version()` export
  (RFD 0001 #2). Tests use a hand-encoded fixture wasm to exercise
  the full Install → Check (`health()`) path without external tooling.
- `internal/plugin/oci`: OCI-tier loader. Runtime detection order
  Podman → Docker → `SAMUEL_RUNTIME` env override; image-name regex
  ported from `samuel_v1/internal/core/docker.go:60-75`; canonical
  mount layout (`/workspace`, `/skills`, `/.samuel/run`,
  `/plugin/config`, `/samuel-bridge`); `--user $UID:$GID`,
  env-var allowlist filter, deny-by-default network policy
  (`--network none` unless outbound capability granted).
- `internal/plugin/oci/bridge` + `api/proto/plugin/v1/plugin.proto`:
  per-container Unix-socket gRPC bridge protocol per RFD 0001
  resolution. v2.0 ships JSON-over-Unix-socket as the wire transport
  to land end-to-end tests today; generated gRPC bindings ride v2.1
  alongside the first real OCI plugin (claude-runner).
- `internal/plugin/service`: install-side facade that orchestrates
  registry resolve → source fetch → signature verify → capability
  decide → tier-specific Install → samuel.lock + samuel.toml record.
  Handles uninstall replay, `ListInstalled` / `ListAvailable`, and the
  Update-flow refresh.
- CLI commands `samuel install [<plugin>[@version-range]]`,
  `samuel uninstall <plugin>`, `samuel ls [name]` (`--all`, `--type`),
  `samuel search <query>`, `samuel info <plugin>`,
  `samuel update [<plugin>]` (`--all`). Each supports `--json`.
  Smart bare invocation: `samuel install` with no plugin name lists
  installed plugins and points to `samuel search`.

### Notes

- Sigstore (`sigstore-go`) integration ships in v2.1; v2.0 uses a
  policy-aware `StubVerifier` that honors `[security]` /
  `--allow-unsigned` so users can install today.
- Generated gRPC bindings (protoc-gen-go-grpc) for the OCI bridge
  ride v2.1; the wire format on the socket is JSON-over-Unix-socket
  with the same envelope schema as the proto messages.

## [v2.0.0-alpha.2] — Core (Milestone 2)

PRD: [0002-prd-core.md](.samuel/tasks/0002-prd-core.md)

### Added

- `plugin.Mutation` audit log: serialized to `samuel.lock` so `samuel
  uninstall` can reverse what was applied without rerunning Detect on
  every plugin. New mutation kinds: `wasm_loaded`, `oci_pulled`,
  `lock_entry_written`.
- `internal/lock/lockfile.go`: convenience reader/writer (`ReadLockfile`,
  `WriteLockfile`, `RecordMutations`) layered on top of
  `internal/config` so the advisory flock and the mutation-record
  lockfile share a single TOML file but live in distinct packages.
- `internal/orchestrator`: declared-order Install with LIFO rollback on
  a fresh context (`rollbackTimeout = 30s`), reverse-order Uninstall
  joined via `errors.Join`, and `Doctor` that runs Check without the
  install lock. Rollback failures are wrapped non-recoverably with
  `SAM-ROLLBACK-001` DocsURL.
- `internal/builtins`: embedded four built-in skills (`ralph`,
  `create-skill`, `sync`, `generate-agents-md`) via `//go:embed
  all:content`. Each ships a SKILL.md placeholder following the Agent
  Skills standard.
- `internal/components/samuel`: first concrete `plugin.Plugin`. Syncs
  the embedded built-ins into `~/.samuel/builtins/` with content-hash
  idempotency, atomic sibling-tmp+rename swap, and a path-traversal
  defense using `filepath.IsLocal`.
- `internal/sync`: per-folder AGENTS.md generator ported from v1.
  AGENTS.md-only (CLAUDE.md emission dropped per RFD 0009). Autogen
  marker (`<!-- Auto-generated by Samuel`) preserved; defaults
  user-overridable via `samuel.toml [sync.*]`. Hook stubs defined for
  PRD 0004 methodology bodies.
- `samuel init [project-name]`: writes `samuel.toml`, creates
  `.samuel/{tasks,builtins,plugins}/`, runs `SamuelComponent.Install`,
  renders root AGENTS.md, walks per-folder sync. Refuses to run inside
  Samuel's own repo. Flags: `--force`, `--minimal`, `--yes`,
  `--non-interactive`, `--json`. Smart bare invocation: already-init'd
  projects print status and exit 0.
- `samuel doctor`: per-plugin `✓/✗` rendering with summary counts.
  `--fix` re-installs plugins whose Check reports unhealthy.
  Suggests translator plugins for assistants found on `PATH` (RFD
  0002 §1). Detects unmanaged v1 `~/.claude/skills/` content.
- `samuel sync`: standalone command for the per-folder AGENTS.md
  walker. `--dry-run`, `--force`, `--max-depth`, `--json`. Smart bare
  invocation previews when run uninitialized.
- `samuel.toml` schema validation on Load: required `version`,
  `default_methodology` resolvable, `[[plugins]]` kind enum check.
  Errors carry `SAM-CFG-010` … `SAM-CFG-012` DocsURLs.

### Changed

- `internal/plugin`: `MutationOciPull` → `MutationOciPulled`,
  `MutationWasmCache` → `MutationWasmLoaded` to match the wider
  enum-naming convention. Added `MutationLockEntryWritten`.
- v2 framework is `.claude/`-agnostic (RFD 0009): no command writes to
  `.claude/`, `~/.claude/`, or `CLAUDE.md`. Verified by an
  end-to-end test that walks both `$HOME` and the project tree.

### Smoke verified

- `samuel init <dir> --yes` produces the expected layout (root
  AGENTS.md, `.samuel/{tasks,builtins,plugins}/`, builtins mirror).
- `samuel doctor` reports healthy after init; `--fix` repairs a
  manually-deleted `~/.samuel/builtins/`.
- `samuel sync` regenerates per-folder AGENTS.md without touching
  user-customized files; `--force` overwrites them.
- `samuel init` refuses to run inside the Samuel source repo with a
  structured error.

## [v2.0.0-alpha.1] — Foundation (Milestone 1)

PRD: [0001-prd-foundation.md](.samuel/tasks/0001-prd-foundation.md)

### Added

- Repository scaffold + Cobra CLI shell (`samuel version`).
- TOON encoder/decoder (`internal/encoding/toon`).
- Cross-process advisory lock (`internal/lock`, flock(2)).
- Structured error type + JSON envelope (`internal/errors`,
  `internal/ui`).
- Initial `Plugin` interface and three placeholder kinds (SkillPlugin,
  WasmPlugin, OciPlugin) in `internal/plugin`.
- CI workflow + goreleaser config (homebrew tap disabled for the v2
  alpha line).

[Unreleased]: https://github.com/samuelpkg/samuel/compare/v2.0.0-alpha.2...HEAD
[v2.0.0-alpha.2]: https://github.com/samuelpkg/samuel/compare/v2.0.0-alpha.1...v2.0.0-alpha.2
[v2.0.0-alpha.1]: https://github.com/samuelpkg/samuel/releases/tag/v2.0.0-alpha.1
