---
title: Samuel Wiki Log
type: log
created: 2026-05-12
updated: 2026-05-13
---

# Wiki Log

## [2026-05-13] roadmap | next 5 PRDs drafted (0007–0011)

With v2.0.0-rc.16 shipped and the Tetris fixture PRD complete, drafted the next batch of framework PRDs from the wiki's open backlog ("What's left after rc.15" in [[synthesis/v2-rc-cycle-lessons]] + the open questions in [[CLAUDE]]):

- **PRD 0007 — Live-registry e2e tier** (v2.0.1). Closes [Issue #10](https://github.com/samuelpkg/samuel/issues/10). Stands up `e2e/live/` against a real `samuel-test-registry` to cover the `fetchGit` path the hermetic tier can't reach. Five fixture plugins per the rc.6 + rc.9 matrix; nightly CI with auto-issue on red.
- **PRD 0008 — Real Sigstore verifier (v2.1.0)**. Closes [Issue #6 final](https://github.com/samuelpkg/samuel/issues/6). Swaps `verify.StubVerifier` for `sigstore-go`. Wire format and lockfile schema stable across the transition. `verify.IsProduction()` flips to true; doctor advisory drops. New RFD 0009.
- **PRD 0009 — WASM plugin tier (v2.2.0)**. Completes wazero embedding + capability enforcement at WASI boundary. <50ms cold-start budget gated in CI. TinyGo first; reference plugin `samuel-go-guide-wasm` exercises the full path. `samuel new plugin --kind=wasm` scaffolding. New RFD 0010.
- **PRD 0010 — OCI plugin tier + network policy (v2.3.0)**. Resolves the long-open network-policy question: **deny-by-default with per-call consent prompts** (recorded in PRD's Decision section). Container runtime detection (Podman-rootless → Podman → Docker), reference plugin `samuel-claude-code-oci` packaging Claude Code as an OCI plugin, `samuel run --sandbox=oci` for agent containerization. New RFD 0011.
- **PRD 0011 — v1 skill migration execution (v2.3.x rolling)**. The mechanical conversion of 78 v1 skills per RFD 0007's plan: 4 built-in (verify), 12 starter-pack meta-plugin, 58 pure plugins, 2 dropped. Migration script + repo provisioning script + signing workflow templates. Populates the registry from near-empty to ~71 entries.

Ordering: 0007 → 0008 → 0009 → 0010 → 0011. Each lands a clean version. PRDs 0009 and 0010 introduce `samuel new plugin --kind=*` scaffolding (shared work across the two PRDs); PRD 0011 depends on both for ecosystem credibility but its mechanical surface area is independent.

Dropped from this batch (worth tracking, lower priority): private plugin registry auth (wait for real user demand), RFDs 0009–0012 written alongside their implementing PRDs rather than ahead.

PRD files: `samuel/.samuel/tasks/0007-prd-live-registry-e2e.md` through `0011-prd-skill-migration-execution.md`.

## [2026-05-13] ingest | v2 rc.2 → rc.15 cycle lessons

Samuel v2 shipped at rc.2 on 2026-05-12 ("Public release"). Within 24 hours a manual-test sweep against a fresh `samuel init` fixture surfaced 9 issues and forced 13 more release candidates before the codebase reached genuine ship-ready state.

Created:

- `synthesis/v2-rc-cycle-lessons.md` — full taxonomy of the bug patterns (schema drift, cache-key gaps, dispatch gaps), the Claude carve-out architectural shift, default-on-helper pattern, verifier-stub disclosure, and the structural changes (`examples/tetris/` fixture + `e2e/hermetic/` suite) that close the recurrence gap.

Updated:

- `CLAUDE.md` — domain section now reflects "v2 shipped at rc.15" not "empty target." "Resolved scope decisions" line about translator plugins corrected for the Claude carve-out. Signing line annotated with the v2.0 StubVerifier reality + v2.1 path.
- `index.md` — synthesis section gains the new rc-cycle lessons page.

Key findings:

- **Schema drift between registry generator and parser** (rc.3) — registry emitted `[[plugins]]`, parser expected `[plugin.<name>]`. TOML parsed cleanly with zero plugins. Caught only by manual testing because unit tests used the writer's own shape.
- **The Claude carve-out** (rc.4) — every major coding assistant except Claude reads `AGENTS.md` natively. The "no tool-specific code in core" invariant relaxed for the Claude translator only; everything else (Cursor, Codex specifics, future tools) stays in plugins. `#v2-decision`
- **Cache keys must cover every input that affects the result** (rc.7) — verify cache keyed on digest only; `--allow-unsigned` decisions became sticky forever. Cache key now `(digest, AllowUnsigned)`.
- **New state tiers must propagate to every dispatcher** (rc.14) — `plugin:<name>` health checks added in rc.11, but `doctor --fix` and the post-fix re-check loops both queried only the orchestrator. Took manual testing to surface.
- **Hermetic e2e cannot fully replace live-registry e2e** — `file://` URLs route through `source.fetchFile`, not `source.fetchGit`. The hermetic tier (rc.15) covers ~80% of the manual sweep; the rc.6 (v-prefix tag fallback) and rc.9 (.git strip) fixes need the live-registry tier (Issue #10) for CLI-surface coverage.

## [2026-05-12] scope | TOON adoption for run runtime files

User direction: more aggressive TOON adoption than my initial conservative pitch. Prior v1 usage showed JSON-emission breaking in practice (missing commas, unescaped quotes, manual repair required). The theoretical "JSON is reliable" argument doesn't hold empirically.

Resolved:
- **`prd.toon` replaces `prd.json`** at v2.0 launch. Tabular tasks array is TOON's native sweet spot. Line-oriented rows tolerate malformations better than nested JSON.
- **All `.samuel/run/` structured files use TOON**: `prd.toon`, `project-snapshot.toon`, `task-context.toon`.
- **Append-only logs stay markdown** (`progress.md`, `progress-context.md`).
- **Agent stops writing prd directly** — uses CLI subcommands (`samuel run done|skip|reset|enqueue`). Samuel CLI is the only mutator. This is the load-bearing decoupling — encoding becomes a Samuel-internal concern.
- **Default `structured = "toon"`** in `samuel.toml`, with `"json"` as escape hatch.
- **TOON version pinned** in `samuel.lock` and embedded in each `.toon` file header.
- **Write our own Go TOON encoder/decoder** if no maintained library exists by build time. Spec is small enough (~200 LOC).

Updated:
- `concepts/toon-evaluation.md` — corrected analysis (initial JSON-reliability claim rejected by user observation).
- `entities/auto-prd.md`, `entities/auto-runtime-files.md` — v2 layout reflects TOON adoption.
- `synthesis/auto-mode-v2-design.md` — encoding section added.
- CLAUDE.md — resolved decisions block extended.

## [2026-05-12] lint + agnostic audit | wiki health pass

Ran the llm-wiki lint check + a focused agnostic-by-design audit.

**Lint results:**
- Orphans: zero.
- Index drift: clean.
- Broken links: 3 real (false positives from TOML `[[plugins]]` syntax inside code blocks filtered out).

**Fixes applied:**
- Created `entities/config-format.md` (the TOML decision was referenced as `[[entities/config-format]]` from two pages but the entity didn't exist).
- Fixed stale link in CLAUDE.md (`samuel-v1-readme` → `v1-project-meta`).
- Updated `entities/samuel-v1.md` with full internal version timeline (1.0.0 → Unreleased) and explicit note that `samuel auto` → `samuel run` already happened in v1's own v3.0.0.

**Agnostic audit — user-requested verification that v2 is truly agent-agnostic.**

Four issues found and fixed:
1. `concepts/prompt-template-variables.md` exposed `ClaudeMD string` in `PathsInfo` — removed; tool-specific paths come from translator plugin namespaces only.
2. `synthesis/v2-command-tree.md` described `samuel sync` as "the per-folder CLAUDE.md generator" — fixed to AGENTS.md.
3. `concepts/per-folder-context.md` title was "auto-generated CLAUDE.md / AGENTS.md" — fixed to AGENTS.md only.
4. Created `concepts/agnostic-by-design.md` as a load-bearing invariant page with proposed CI check.

**Audit verdict**: v2 framework is structurally agnostic. The framework binary writes only `AGENTS.md`, `samuel.toml`, `.samuel/run/*`. All tool-specific files (`CLAUDE.md`, `.claude/settings.json`, `.cursor/rules/*`) are managed by translator plugins (`claude-translator`, `cursor-translator`, etc.) — symmetric, none privileged. Default `agent = "claude"` in samuel.toml is a default, not lock-in. Built-in adapters for 5 agents (Claude, Codex, Copilot, Gemini, Kiro) carry forward from v1's `docker.go`. Same interface; user picks.

**Wiki state post-lint**: 33 sources + entities + concepts + synthesis. All links resolve. Agnostic invariant explicit and enforceable via CI.

## [2026-05-12] bootstrap | wiki created

- Scaffolded directory tree (`raw/`, `sources/`, `entities/`, `concepts/`, `synthesis/`, `queries/`).
- Wrote starter `CLAUDE.md` scoped to the Samuel v1 → v2 rebuild.
- Wrote empty `index.md`.

## [2026-05-12] scope | resolved v2 decisions

- Runtime: Go.
- Release: clean break, deprecate v1 on release.
- Extensibility: framework + skills hub via pluggable plugins/skills.
- Wrote 15-step bottom-up ingest plan into `CLAUDE.md`.

## [2026-05-12] scope | v2 positioning + plugin/version recommendations

User filed major v2 decisions:
- Positioning: "Rails for coding assistants" — package manager + task executor with baked-in methodology.
- Built-in core = thin: cross-tool primitives only (AGENTS.md, SKILL.md, design.md, loader, methodology hooks).
- Plugin discovery = dual: local + GitHub-backed registry (just a repo with an index).
- User asked for recommendations on plugin format/sandbox and versioning. Filed:
  - `concepts/plugin-format.md` — two-tier (text-skills + OCI-image plugins), GHCR distribution, container sandbox per assistant run.
  - `concepts/versioning-compatibility.md` — SemVer 2.0.0, cargo-style ranges, three version axes (framework / protocol / plugin), capability permission model.
- Filed `concepts/extensibility-design.md` and `synthesis/positioning-rails-for-coding-assistants.md`.
- Updated `entities/samuel-v2.md` with the new positioning + decisions.

Pending user confirmation: OCI-image recommendation; SemVer + capability model.

## [2026-05-12] scope | three-tier plugin model + TOML + Sigstore confirmed

User responses to open questions:
- Container-runtime requirement: explore embedding. Honest answer filed — true container runtime can't be embedded across OSes (kernel-bound), but WASM via wazero (pure Go) can. Resolved as **three-tier plugin model**: skills (text, no deps) / WASM (embedded sandbox) / OCI (host Docker/Podman, only for heavy/assistant exec).
- Signing: Sigstore confirmed, signed-by-default for official registry.
- Config: TOML default, YAML supported, SKILL.md frontmatter stays YAML for Agent Skills compat.

Updated:
- `concepts/plugin-format.md` — rewritten with three tiers, embed-runtime analysis, TOML manifest examples.
- `concepts/versioning-compatibility.md` — TOML manifest sketch, Sigstore confirmation, lockfile as TOML.
- `entities/samuel-v2.md` + `CLAUDE.md` — resolved decisions, refreshed open questions.

## [2026-05-12] scope | methodology pattern = default built-in + plugin enhancement

User confirmed: every Samuel workflow ships a default ("Samuel Way") built into the framework, and plugins enhance individual stages rather than replace whole workflows. Filed [[concepts/methodology-default-plus-plugin]] with hook-point sketch. Applied to auto-mode in pass 3 — see [[synthesis/auto-mode-v2-design]].

## [2026-05-12] scope | auto-mode rename + prompt template spec + discovery-only confirmed

User decisions:
- **CLI rename**: `samuel auto` → `samuel run [methodology]`. Default methodology = `ralph` (Ralph Wiggum loop). Aliases supported (`rw`). v1 already has a `samuel run` command — reconcile in pass 6.
- **Runtime dir rename**: `.claude/auto/` → `.samuel/run/` (or `.samuel/<methodology>/` for multi-methodology projects).
- **Discovery-only mode**: confirmed as built-in feature (`samuel run --discover-only`).
- **Prompt template variables**: filed [[concepts/prompt-template-variables]] with full spec. Built-in template + per-project override at `.samuel/templates/<methodology>/` + plugin override. Variables grouped: Samuel, Project, Methodology, Iteration, Config, Guardrails, Paths, State, Mode, Hooks, Plugins. Guardrails moved out of prompt text into config.

Updated: [[synthesis/auto-mode-v2-design]] with rename + template ref. CLAUDE.md resolved-decisions block expanded.

## [2026-05-12] ingest | Pass 12: v1 project meta — INGEST COMPLETE

Files: `README.md`, repo-root `CLAUDE.md` + `AGENTS.md`, `CHANGELOG.md`, `LICENSE`.

Created:
- `sources/2026-05-12-v1-project-meta.md` — final source page

Findings:
- **README hero is strong**: "samuel run and walk away. Ralph Wiggum methodology • Cross-tool • Opinionated guardrails baked in." Hero rewrite documented in v3.0.0 CHANGELOG ("response to /autoplan CEO + DX phase findings" — v1 dogfooded structured planning to plan its own release).
- **Repo-root CLAUDE.md/AGENTS.md DIFFER from template/.** Root: 340/316 lines. Template: 474/474. Root is leaner — closer to RFD 0001's ~280 target. v1 has the lean version; it just isn't what users get. v2 should ship the lean shape.
- **CHANGELOG is best-in-class.** 12 releases over 15 months. v3.0.0 entry is exceptional: thesis upfront, rationale per item, specific filenames in Internal section, 60+ new tests cited, concrete deprecation timeline (~3 months). Set as the v2 standard.
- **Internal version timeline reconciled**: v1.0.0 (2025-01-14) → v1.8.0 (Agent Skills) → v2.0.0 (`.agent/` → `.claude/`, single CLAUDE.md) → v3.0.0 (lean reshape, `auto` → `run`) → [Unreleased] = what we've been ingesting. The user's "external v1" maps to internal "post-3.0.0 + Unreleased changes." The user's "v2 rebuild" is conceptually internal "v4.0.0."

## Ingest summary

12 passes complete. 32 sources created. Full v1 mapped:
- internal/{skills,core,orchestrator,github,ui,commands}/* (Go code)
- cmd/samuel/* (CLI entry)
- .claude/* (runtime: settings, hooks, dogfood prd.json + progress.md)
- internal/skills/content/* + .claude/skills/* (78 skills, byte-identical mirrors)
- template/* (3-file install template)
- docs/* + mkdocs.yml (mkdocs-material docs site)
- rfd-index.yaml + docs/rfd/0001-0004.md (RFDs)
- Makefile + .goreleaser.yaml + install.sh + .github/workflows/* (build + release)
- README.md + root CLAUDE.md + root AGENTS.md + CHANGELOG.md + LICENSE (project meta)

Next: run a `lint` pass on the wiki to catch any contradictions, broken links, or coverage gaps before declaring done.

## [2026-05-12] ingest | Pass 11: v1 build + release

Files: `Makefile`, `.goreleaser.yaml`, `install.sh`, `.github/workflows/{ci,release}.yml`.

Created:
- `sources/2026-05-12-v1-build-release.md`

Findings:
- Solid off-the-shelf release infrastructure. 5 platforms (darwin/linux × amd64/arm64 + windows/amd64), static CGO-disabled binaries, conventional-commits-grouped changelog, Homebrew tap with auto-generated shell completions, POSIX install.sh with curl/wget fallback.
- `replace_existing_artifacts: true` is a hard-earned lesson — caught during v3.0.0 ship when auth issue on homebrew step made the workflow non-idempotent. Inline comment preserved as institutional memory.

v2 additions (per earlier wiki decisions):
- Cosign signing per [[concepts/versioning-compatibility]].
- OCI image publication to ghcr.io for the framework binary itself.
- SBOM + SLSA L2 provenance via goreleaser built-in.
- AGENTS.md template line check in CI (RFD 0001 lesson — enforce with CI, don't just target).
- Reusable plugin-release workflow for the plugin ecosystem.

Open: register `samuel.dev` (referenced in v1 error DocsURLs but currently unregistered).

## [2026-05-12] ingest | Pass 10: v1 RFDs

Files: `rfd-index.yaml`, `docs/rfd/{index,0001,0002,0003,0004}.md`.

Created:
- `sources/2026-05-12-v1-rfds.md`
- `concepts/rfd-process.md` — states, frontmatter, body structure
- `synthesis/v2-rfds-to-write.md` — eight inaugural v2 RFDs

Findings:
- **RFD process is polished.** Four committed RFDs (progressive disclosure, idiomatic Go layout, composable CLI, smart interactive mode). State machine well-defined. Inspired by Oxide / IETF RFC tradition.
- **RFD 0001 is a cautionary tale.** v1 set target of ~280 lines for CLAUDE.md 18 months ago. Today's template is 474 lines — drifted back. v2 needs CI enforcement.
- **RFD 0004 pattern worth porting**: skip interactive prompts when CLI flags fully specify operation (not `--non-interactive`, not TTY detection — flag detection).
- **RFD vs PRD distinction**: RFD = "Why + What options"; PRD = "What + How". Flow: Idea → RFD → PRD → Tasks → Code.

v2 plan: write eight inaugural RFDs in `samuel/docs/rfd/` covering plugin architecture, AGENTS.md primary, SemVer + capabilities, methodology hooks, component-lifecycle, run rename, plugin migration, drop gstack/gbrain. List in [[synthesis/v2-rfds-to-write]].

## [2026-05-12] ingest | Pass 9: v1 template + docs

Files: `template/{CLAUDE,AGENTS}.md`, `template/.claude/auto/prompt.md`, `mkdocs.yml`, sample reads of `docs/{core,getting-started,reference}/`.

Created:
- `sources/2026-05-12-v1-template-docs.md`
- `concepts/4d-methodology.md` — Samuel Way surfaces here for the first time
- `synthesis/v2-template-and-docs.md` — proposed shrinkage + docs restructure

Findings:
- **`template/` is just 3 files.** Skills come from embed, not template.
- **CLAUDE.md template is 474 lines** — ~7-8K tokens loaded into every agent context. v2 target: ~100 lines AGENTS.md by externalizing guardrails to `samuel.toml`, dropping skill links, moving reference to docs.
- **4D Methodology surfaces**: Deconstruct/Diagnose/Develop/Deliver × ATOMIC/FEATURE/COMPLEX modes. Threads through every Samuel workflow.
- **Three-way duplication**: skill content lives at `internal/skills/content/` + `.claude/skills/` + `docs/<category>/`. v2 collapses to one source of truth per plugin.
- **mkdocs-material** is the right stack — keep. Drop the languages/frameworks/workflows trees (duplicates). Add `docs/concepts/` and `docs/plugin-authors/`.
- **Embedded changelog** in CLAUDE.md reveals v1's predecessor format: `.agent/` (v1.x), `AI_INSTRUCTIONS.md` + `CLAUDE.md` + `project.md` (consolidated into v2.0). Multiple prior iterations.

## [2026-05-12] ingest | Pass 8: v1 skill content survey

Files: 78 skills under `.claude/skills/` (byte-identical mirror at `internal/skills/content/`). Sample reads: go-guide, react, create-rfd, mcp-builder.

Created:
- `sources/2026-05-12-v1-skill-content-survey.md` — full catalog + per-skill triage
- `synthesis/v2-skill-migration-plan.md` — concrete migration approach

Findings:
- **78 skills, byte-identical mirror.** v1 maintains both trees.
- **Quality is high** across the samples. Auto-load via `metadata.extensions`.
- **Anthropic community skills** (7) bundled in v1; v2 installs on demand.

v2 triage into four buckets:
- **Built-in framework (4)**: `auto` → ralph methodology; `create-skill` → CLI command content; `sync-claude-md` + `generate-agents-md` → `samuel sync` (AGENTS.md primary).
- **Starter-pack plugins (12)**: the Samuel Way workflows. Bundled in `samuel-starter` meta-plugin, auto-installed on `samuel init` unless `--minimal`. (create-rfd, create-prd, generate-tasks, code-review, commit-message, document-work, refactoring, security-audit, testing-strategy, troubleshooting, cleanup-project, dependency-update)
- **Pure plugins (58)**: 21 language guides + 30 framework guides + 7 Anthropic community.
- **Drop (2)**: `initialize-project`, `update-framework` (replaced by built-in commands).

Migration approach: one Git repo per plugin, separate `samuel-registry` index repo, cosign per plugin. ~4-5 days effort, mostly scriptable.

## [2026-05-12] ingest | Pass 7: v1 CLI entry + .claude/ runtime

Files: `cmd/samuel/main.go`, `cmd/CLAUDE.md`, `.claude/settings.json`, `.claude/hooks/check-gstack.sh`, plus dogfood `.claude/auto/{prd.json, progress.md}`.

Created:
- `sources/2026-05-12-v1-cli-entry-runtime.md`
- `concepts/claude-code-hooks.md` — agent-boundary enforcement pattern

Findings:
- **`cmd/samuel/main.go` is 18 lines.** Standard Go layout, ports verbatim.
- **`.claude/settings.json` registers a Claude Code PreToolUse hook** — Claude Code lets users gate tool calls via shell commands. v1 uses this to deny Skill usage when gstack isn't installed: `{"permissionDecision": "deny", "message": "..."}`.
- **The pattern is `#rescue`** even though gstack drops. Use cases for v2: deny reads outside `/workspace`, audit Bash commands, require user confirmation on sensitive writes. Filed as a translator-plugin concern — `claude-translator` owns `.claude/settings.json`, not the framework.
- **Dogfood proof**: v1's own repo has `.claude/auto/prd.json` with 60+ tasks (many completed with commit SHAs) and 1034 lines of `progress.md`. Pilot mode found and fixed real bugs in v1 itself (file.Close error checks, Docker image validation). The methodology has shipped real value.
- **Two layers of "hooks" in Samuel** confirmed: (a) Samuel methodology hooks (built into v2 framework, see [[concepts/methodology-default-plus-plugin]]); (b) Claude Code's PreToolUse hooks (a Claude Code feature, used via translator plugins).

## [2026-05-12] scope | init/new + AGENTS.md primary + sync as hook

User decisions on pass-6 open questions:
- **Keep `init`, not `new`** — works for both new and existing projects (matches `git init`, `npm init`). Reserve `new` for any future create-only verb.
- **AGENTS.md primary, not CLAUDE.md.** v2 framework writes AGENTS.md by default. Tool-specific files (CLAUDE.md, Cursor rules, Continue rules) come from translator plugins. This aligns with "Rails for coding assistants" plurality.
- **Sync as hook + command.** Same code path, two trigger surfaces. Runs automatically at lifecycle points (init, install, optional per-iteration) AND manually via `samuel sync`.
- **`samuel plugin` subcommand confirmed.**

Created:
- `concepts/agents-md-primary.md` — fundamental positioning, agent-agnostic by default

Updated:
- `concepts/per-folder-context.md` — AGENTS.md only, translator plugins for tool-specific
- `synthesis/v2-command-tree.md` — resolved init/new + sync hook decisions
- CLAUDE.md, index.md — reflect

## [2026-05-12] ingest | Pass 6: v1 commands layer

Files: `internal/commands/*` — 33 non-test files, ~7,000 LOC. Deep-read: root, auto, init, doctor, add, skill, legacy, config_cmd. Skimmed the rest.

Created:
- `sources/2026-05-12-v1-commands.md`
- `entities/command-tree-v1.md` — full v1 surface
- `concepts/smart-bare-invocation.md` — never silently start work
- `concepts/json-mode-everywhere.md` — every command emits `--json`
- `synthesis/v2-command-tree.md` — proposed v2 CLI surface

Big findings:
- **v1 ALREADY renamed `samuel auto` → `samuel run` in v3.0.0**, with `auto` kept as a permanent alias. Our [[synthesis/auto-mode-v2-design]] rename decision was independently aligned. v2 goes further with `samuel run [methodology]`.
- **Smart bare invocation pattern**: `samuel run` with no args shows status if PRD exists, otherwise prints actionable help to stderr and exits non-zero. **Never silently starts a loop.** Filed as a general pattern for any mutating verb.
- **Deprecation/redirect pattern is elegant**: `redirectAndRun(newPath, handler)` wraps a handler with stderr deprecation note. Suppressible via env or flag. Stderr-only so JSON consumers aren't affected. Worth keeping the utility even though v2 is a clean break (future renames will want it).
- **JSON mode is universal**: every command implements `JSONMode(cmd)` + structured output. The discipline is consistent — `#rescue` as a v2 invariant.
- **v2 command tree proposal**: 12 top-level commands vs v1's ~20. ~40% reduction by collapsing language/framework/workflow → plugins, dropping gstack/gbrain, dropping legacy aliases.
- Renames suggested: `add` → `install`, `remove` → `uninstall`, `admin sync` → `sync` (promoted).
- New: `samuel plugin` subcommand for plugin authors (`new`, `build`, `publish`, `validate`, `verify`).

## [2026-05-12] ingest | Pass 5: v1 github + ui

Files: `internal/github/client.go`, `internal/ui/{output,json,prompts,spinner}.go`.

Created:
- `sources/2026-05-12-v1-github-ui.md`
- `entities/github-client.md`, `entities/ui-package.md`

Smaller pass — utility packages.

Findings:
- **github/client.go** is mostly `#drop` for v2 (per-plugin fetch, not Samuel-repo coupling), but the download primitives — 30s timeout, 10MB cap, User-Agent, limit-reader, v-prefix stripping — port directly.
- **JSON envelope with schema versioning** in `ui/json.go` is the cleanest pattern in v1's UI. `JSONSchemaVersion = 3` constant carries an inline diff comment. Worth preserving (bump to 4 for v2).
- **UI shape `#rescue`, libraries `#refactor`.** Recommendation filed: swap `fatih/color` → `charmbracelet/lipgloss`, `manifoldco/promptui` → `charmbracelet/huh`, `schollz/progressbar/v3` → `charmbracelet/bubbles`. Same API surface, modern internals. Native multi-select in huh replaces v1's awkward custom impl.

## [2026-05-12] scope | gstack + gbrain dropped from v2

User decision: neither survives v2. May reintroduce as plugins later or extract reusable pieces as skills. v1 product opinions, not framework essentials. v2's `samuel init` becomes much smaller: framework binary + built-in skills + lockfile setup.

Updated: [[entities/component-gstack-gbrain]] retagged `#drop`. CLAUDE.md + index.md reflect.

## [2026-05-12] ingest | Pass 4: v1 orchestrator

Files: `internal/orchestrator/{orchestrator,component,component_samuel,component_gstack,component_gbrain,errors,lock_unix}.go`.

Created:
- `sources/2026-05-12-v1-orchestrator.md`
- `entities/orchestrator.md`, `entities/component-samuel.md`, `entities/component-gstack-gbrain.md`
- `concepts/component-lifecycle.md`, `concepts/structured-errors.md`
- `synthesis/orchestrator-as-plugin-loader.md`

Big findings:
- **Highest-engineering-quality subsystem in v1.** Component interface (Detect/Install/Check/Uninstall), Mutation/Reverse log with LIFO rollback, structured errors with Problem/Cause/Fix/DocsURL, flock(2) advisory locking, atomic-swap pattern (tmp+rename+backup-restore), rollback context separation, best-effort uninstall via errors.Join.
- **The orchestrator IS v2's plugin loader.** The mapping is 1:1: Component → Plugin, Mutation → samuel.lock entries, Error → plugin SDK error type, flock → same flock. Porting is the right call; redesign would lose value.
- **Three concrete components**: `SamuelComponent` (rescue — sync embedded skills, atomic content-hash idempotency, path-traversal-defended), `GstackComponent` (open — composes external `garrytan/gstack`, pinned-SHA pattern), `GbrainComponent` (open — registers MCP server via `claude mcp add`).
- **Open question for user**: do gstack and gbrain survive v2, or were they v1 product opinions? If they survive, they become plugins, not built-ins.
- **The pinned-SHA pattern** in GstackComponent generalizes — applies to any plugin manifest that pins an external repo.

Build order for v2's plugin layer sketched in [[synthesis/orchestrator-as-plugin-loader]]: errors+lock first, then Component+Orchestrator core, then SamuelComponent as built-in framework bootstrap, then Manifest parsing, then the three plugin-kind adapters (skill/WASM/OCI), then dependency resolver, then Sigstore.

## [2026-05-12] ingest | Pass 3: v1 auto-mode

Files (11 Go files, ~2.5k lines): `internal/core/auto*.go` plus runtime template `samuel_v1/.claude/auto/prompt.md`.

Created:
- `sources/2026-05-12-v1-auto-mode.md`
- `entities/auto-prd.md`, `entities/auto-loop.md`, `entities/auto-runtime-files.md`, `entities/auto-prompts.md`
- `concepts/ralph-wiggum-methodology.md`, `concepts/pre-computed-context.md`, `concepts/pilot-mode.md`
- `synthesis/auto-mode-v2-design.md`

Big findings:
- **Auto-mode is the flagship methodology**, well-designed, references Geoffrey Huntley's "Ralph Wiggum" pattern by name.
- **The pre-computed context pattern is v1's real innovation.** Samuel regenerates three small md files before each iteration; the prompt tells the agent "read these first, don't grep". Token-discipline as a first-class concern. `#rescue` and elevate to a v2 framework concept.
- **Two modes**: implementation (predefined tasks) + pilot (LLM discovers tasks). Discovery iterations are forbidden from making code changes — separation of analysis and execution.
- **Multi-agent already at the data model level**: `IsValidAITool` allowlists claude/amp/cursor/codex. Per-tool prompt translation in `requiresPromptContent`.
- **Safety primitives are strong**: atomic save (write-temp-then-rename), AI-tool allowlist (defense against prd.json injection), image regex validation, consecutive-failure abort, progress.md rotation.
- **AI-output resilience**: custom UnmarshalJSON handles numeric IDs (agents sometimes emit `"id": 1` instead of `"id": "1"`).

Tentative v2 hook surface filed in [[synthesis/auto-mode-v2-design]] — `before:iteration`, `iteration.gate`, `context.{snapshot,progress,task,extra}`, `agent.invoke`, `quality.check`, `after:iteration`, `after:loop`.

## [2026-05-12] ingest | Pass 2: v1 config + sync

Files: `internal/core/{config,sync,downloader,extractor,docker}.go`, `samuel.yaml`.

Created:
- `sources/2026-05-12-v1-config-sync.md`
- `entities/config-go.md` (Config + samuel.yaml schema)
- `entities/sync-claude-md.md` (per-folder generator)
- `entities/downloader-extractor.md` (v3-era tarball flow)
- `entities/docker-sandbox.md` (v1 sandbox layer)
- `concepts/per-folder-context.md`
- `concepts/multi-agent-support.md`

Updated: `entities/samuel-v1.md` (added "things v1 already has that v2 should keep" section).

Big findings:
- **`sync.go` is per-folder CLAUDE.md generator, not skill-sync.** `#rescue` candidate — useful, self-contained.
- **`docker.go` already does multi-agent sandbox** — Claude, Codex, Copilot, Gemini, Kiro. Five agents wired through one prompt-translation switch. v2 sandbox path is well-trodden, not green-field.
- **Two install paths coexist** — embed.go (v4) and downloader+extractor (v3 legacy). The latter is `#drop`.
- **Config has same triple-bookkeeping** as registry (four parallel installed lists with migration helpers). `#drop` — collapse to one `[[plugins]]` list in TOML.
- **`~/.config/samuel` is hardcoded** — would break Windows. v2: use `os.UserConfigDir()`.

## [2026-05-12] ingest | Pass 1: v1 skill model

Files: `internal/skills/embed.go`, `internal/skills/README.md`, `internal/core/skill.go`, `internal/core/registry.go`.

Created:
- `entities/samuel-v1.md`, `entities/samuel-v2.md` (anchors)
- `sources/2026-05-12-v1-skill-model.md`
- `entities/skill-md.md`, `entities/skills-embed.md`, `entities/registry.md`
- `concepts/agent-skills-standard.md`, `concepts/skills-architecture-v1.md`

Key findings:
- v1 implements the Agent Skills open standard (agentskills.io). `#rescue` — cross-tool portable to 25+ AI products.
- Registry is **static Go source**. Adding a skill requires editing code + rebuilding. `#drop` — biggest blocker to "skills hub".
- Internal version comment in `embed.go` reveals this v1 is Samuel's *4th* iteration internally.
- Language/Framework/Workflow split is leaky — all stored as Agent Skills, only the registry slices keep them separate.
