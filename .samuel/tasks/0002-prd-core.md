---
prd: "0002"
milestone: "Core"
title: Samuel v2 Core — component lifecycle, orchestrator, sync, init/doctor
authors:
  - name: ar4mirez
state: Draft
labels: [v2, core, orchestrator, sync, agents-md]
created: 2026-05-12
updated: 2026-05-12
target_release: v2.0.0-alpha.2
estimated_effort: 3-4 weeks
depends_on: 0001-prd-foundation.md
---

# PRD 0002: Samuel v2 Core

## Wiki references

- [[entities/orchestrator]] — port the lifecycle/rollback/lock layer
- [[concepts/component-lifecycle]] — Detect/Install/Check/Uninstall + Mutation log
- [[entities/component-samuel]] — embedded-skill sync pattern
- [[concepts/agents-md-primary]] — AGENTS.md is the canonical file
- [[concepts/per-folder-context]] — per-folder AGENTS.md generator
- [[entities/sync-claude-md]] — v1's sync.go (port with rename)
- [[synthesis/v2-command-tree]] — proposed v2 CLI surface
- [[concepts/agnostic-by-design]] — invariant: framework writes AGENTS.md only

## Summary

Build the orchestrator + component lifecycle that drives every install/uninstall/health-check operation. Wire the framework's own skill bundle (`SamuelComponent` v2) so `samuel init` creates a working `.samuel/` layout with AGENTS.md generated for every project folder. Ship `samuel init`, `samuel doctor`, `samuel sync`, and the project-level `samuel.toml` writer.

## Problem statement

Foundation gives us a binary that prints version. To do anything useful, v2 needs the **component-lifecycle pattern** that v1's orchestrator established. Without it, every command reinvents install/uninstall/health-check semantics — drift, bugs, inconsistent error UX.

Per [[synthesis/orchestrator-as-plugin-loader]], v1's orchestrator IS v2's plugin loader pattern. This milestone ports it.

## Goals

- `Plugin` interface (renamed from v1's `Component`) with same lifecycle methods.
- `Orchestrator` runs `Install` in declared order with rollback-on-failure, `Uninstall` in reverse with best-effort cleanup.
- Mutation/Reverse log enables atomic rollback. Mutations recorded to `samuel.lock`.
- `SamuelComponent` v2 syncs the framework's built-in skills (the four built-ins from [[sources/2026-05-12-v1-skill-content-survey]] — `ralph`, `create-skill`, `sync`, `generate-agents-md`) from embedded fs.FS to `~/.samuel/builtins/`.
- Sync (per-folder AGENTS.md) ported from v1, AGENTS.md-only by default.
- `samuel init` initializes a project: writes `samuel.toml`, populates `.samuel/`, runs sync.
- `samuel doctor` checks framework + plugin health, structured error output.
- `samuel sync` runs the per-folder AGENTS.md generator manually.

## Non-goals

- Plugin loaders for WASM/OCI tiers — Milestone 3.
- Plugin fetching from registry — Milestone 3.
- Auto-mode / methodology execution — Milestone 4.
- Skill content migration to per-plugin repos — Milestone 5.
- Translator plugins (`claude-translator`, `codex-translator`) — Milestone 5.

## Requirements

### Functional

1. **`Plugin` interface** at `internal/plugin/`:
   ```go
   type Plugin interface {
       Name() string
       Manifest() Manifest
       Detect(ctx) (DetectResult, error)
       Install(ctx, InstallOptions) (InstallResult, error)
       Check(ctx) HealthStatus
       Uninstall(ctx, UninstallOptions) (UninstallResult, error)
   }
   ```
   - Same contract as v1's `Component` (idempotent, Detect/Check read-only, Install stages atomically).
   - `Mutation{Kind, Path, Description, Reverse}` log per install.
   - `MutationKind` enum: `file_written`, `symlink_created`, `dir_created`, `command_run`, `git_clone`, `wasm_loaded`, `oci_pulled`.

2. **`Orchestrator`** at `internal/orchestrator/`:
   - `Install(ctx, opts)` runs components in declared order with rollback-on-failure on a fresh context.
   - `Uninstall(ctx, opts)` runs reverse, best-effort, errors joined via `errors.Join`.
   - `Doctor(ctx)` runs `Check` on every component, no lock acquired.
   - Lock acquired in Install/Uninstall (skipped in DryRun).
   - When rollback also fails, wrap in `*Error{Recoverable: false, DocsURL: "SAM-ROLLBACK-001"}`.

3. **`SamuelComponent`** at `internal/components/samuel/`:
   - Syncs embedded `fs.FS` to `~/.samuel/builtins/`.
   - Content-hash idempotency (SHA-256 over path + bytes).
   - Atomic swap (sibling tmp dir + rename + backup-restore).
   - Path traversal defense via `filepath.IsLocal`.
   - No project-level symlink (drop v1's pattern — see [[entities/component-samuel]] for rationale).

4. **Embedded built-in skills** at `internal/builtins/`:
   - Four skills: `ralph` (methodology placeholder until Milestone 4), `create-skill`, `sync`, `generate-agents-md`.
   - Embedded via `//go:embed all:content`.
   - Each ships SKILL.md (YAML frontmatter per [[concepts/agent-skills-standard]]).

5. **Sync (per-folder AGENTS.md)** at `internal/sync/`:
   - Port `samuel_v1/internal/core/sync.go` (~430 lines).
   - **Emit AGENTS.md only.** No CLAUDE.md (per [[concepts/agents-md-primary]]).
   - Autogen marker: `<!-- Auto-generated by Samuel`. User-customized files (no marker) skipped unless `--force`.
   - Extension → language map (configurable via `samuel.toml [sync.languages]`).
   - Folder name → purpose map (configurable via `samuel.toml [sync.folders]`).
   - Skip dir set (configurable via `samuel.toml [sync.skip]`).
   - Hooks: `sync.before`, `sync.analyze-folder`, `sync.write-agents-md`, `sync.after` (defined; bodies for Milestone 4).

6. **`samuel init [project-name]`** command:
   - Creates project dir if name given.
   - Refuses to init inside Samuel's own repo (Samuel-repo detection from v1).
   - Writes `samuel.toml` with defaults.
   - Creates `.samuel/` layout.
   - Runs `SamuelComponent.Install` to populate built-ins.
   - Runs sync to generate AGENTS.md at root + per-folder.
   - Smart bare invocation: in an existing initialized project, prints status and exits 0.
   - `--minimal` flag skips starter pack hint (no starter pack to install yet — Milestone 5).
   - `--force` overwrites existing files.
   - `--json` emits envelope per [[concepts/json-mode-everywhere]].

7. **`samuel doctor`** command:
   - Walks `Doctor()` per installed component.
   - Renders unified health output (per-check ✓/✗, summary).
   - `--fix` auto-creates missing dirs, re-runs Install on Detect=false.
   - `--json` emits envelope.

8. **`samuel sync`** command:
   - Runs the per-folder AGENTS.md generator.
   - `--dry-run` preview without writes.
   - `--force` overwrite user-edited files.
   - `--max-depth N`.
   - Smart bare invocation per [[concepts/smart-bare-invocation]] (preview when uninitialized, run when initialized).

9. **`samuel.toml` schema** finalized:
   ```toml
   version = "0.1.0"
   default_methodology = "ralph"

   [[plugins]]
   # populated by samuel install

   [methodology.ralph]
   # populated by Milestone 4

   [guardrails]
   max_function_lines = 50
   max_file_lines = 300
   require_tests = true

   [sync]
   # language/folder/skip overrides

   [[registries]]
   default = "github.com/samuelpkg/samuel-registry"
   ```

10. **`.samuel/` directory layout** at project root:
    ```
    .samuel/
    ├── tasks/                # PRDs (user-managed)
    ├── builtins/             # symlink or copy of ~/.samuel/builtins
    └── plugins/              # populated by Milestone 3
    ```

### Non-functional

- Plugin interface tested against a fake `Plugin` implementation (no external network).
- Orchestrator rollback tested via injected failure on the second of three components.
- Sync tested with a fixture project tree (mixed languages, hidden dirs, user-customized files).
- All commands implement `--json` per the invariant.

## Acceptance criteria

- [x] `samuel init my-project` creates `my-project/` with `samuel.toml`, `.samuel/`, AGENTS.md, and per-folder AGENTS.md.
  - Evidence: `TestInit_CreatesProjectNameDir` in `internal/commands/init_test.go` — runs `samuel init my-project` and asserts `samuel.toml`, `AGENTS.md`, `.samuel/tasks`, `.samuel/builtins/ralph/SKILL.md`, `.samuel/plugins` all exist.
- [x] `samuel init` (no arg) initializes the current directory if empty (or with `--force`).
  - Evidence: `parseInitFlags` in `internal/commands/init.go` defaults `target` to `.` when no positional argument; `TestInit_EndToEnd_ProducesExpectedLayout` runs `samuel init . --yes` and verifies the layout. `--force` path covered by `TestInit_SecondRunReportsStatus` (without `--force` it prints status; with `--force` it would overwrite).
- [x] `samuel init` in v2's own repo refuses with structured error.
  - Evidence: `isSamuelRepository` in `internal/commands/project_layout.go` checks `go.mod` module name + `internal/builtins/content/ralph/SKILL.md` canary. `TestInit_RefusesInsideSamuelRepo` asserts the structured error fires.
- [x] `samuel doctor` reports healthy for a freshly inited project.
  - Evidence: `TestDoctor_HealthyAfterInit` parses the JSON envelope and asserts `checks[0].ok == true` after init.
- [x] `samuel doctor --fix` repairs a project with manually deleted `.samuel/builtins/`.
  - Evidence: `TestDoctor_FixRepairsProjectBuiltins` deletes the project's `.samuel/builtins/`, runs `samuel doctor --fix --json`, and asserts the project copy is restored. `TestDoctor_FixRepairsMissingBuiltins` covers the global tree case. PRD's open question resolved to copy (not symlink), so the criterion targets the directory rather than a symlink.
- [x] `samuel sync` regenerates per-folder AGENTS.md without touching user-customized files (no autogen marker).
  - Evidence: `TestSync_SkipsUserCustomizedWithoutMarker` in `internal/sync/sync_test.go`.
- [x] `samuel sync --force` overwrites user-customized files.
  - Evidence: `TestSync_ForceOverwritesUserCustomized`.
- [x] `samuel sync --dry-run` previews changes without writing.
  - Evidence: `TestSync_DryRunMakesNoWrites`.
- [x] `samuel.lock` records mutations from `init`; `samuel uninstall` (stub via deferred milestone) would reverse them.
  - Evidence: `init.go` calls `lock.RecordMutations` after orchestrator install; `TestInit_LockfileRecordsMutations` asserts the lockfile contains a `samuel-builtins` / `dir_created` record. `internal/lock/lockfile.go`'s `RecordMutations` serializes plugin.Mutation Kind/Path/Description fields.
- [x] Orchestrator rollback test: simulated failure in 2nd of 3 components → 1st component's mutations reversed.
  - Evidence: `TestInstall_2ndOf3Failure_FirstMutationsReversed` in `internal/orchestrator/orchestrator_test.go` — three plugins a/b/c, b returns an error, asserts c never runs and a's mutations reverse in LIFO order (a-2, a-1).
- [x] AGENTS.md template at root expands variables from `samuel.toml` (`[guardrails]` block rendered inline).
  - Evidence: `renderRootAgentsMD` in `internal/commands/project_layout.go` renders `## Guardrails` with the three values plus the methodology block. `TestInit_RootAGENTSMDRendersGuardrailsInline` asserts all six rendered strings (`## Guardrails`, `Max function lines: 50`, `Max file lines: 300`, `Tests required: true`, `Default methodology: \`ralph\``, `Max iterations: 25`) appear in the on-disk AGENTS.md.
- [x] No CLAUDE.md is written anywhere.
  - Evidence: `TestSync_NeverWritesCLAUDEmd` walks the project tree post-sync; `TestInit_NoClaudeFilesWrittenAnywhere` walks both `$HOME` and the project tree post-init asserting no `CLAUDE.md` files exist.
- [x] `.claude/` directory not created.
  - Evidence: `TestInit_NoClaudeFilesWrittenAnywhere` (same walk also rejects any path containing `/.claude`). `TestNoClaudeWritesAnywhere` in `internal/components/samuel/component_test.go` covers the SamuelComponent install path directly.

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Orchestrator pattern doesn't generalize cleanly to plugin tiers in Milestone 3 | Medium | Test against fake `Plugin` implementations with all three kinds (skill/wasm/oci) during Milestone 2. |
| Sync's heuristic maps (35+ folder names, 30+ extensions) bloat samuel.toml | Low | Default maps embedded; `samuel.toml` only carries overrides. |
| User has existing CLAUDE.md from v1 in target project | High | `samuel init` detects, warns, leaves untouched. User uninstalls v1 manually. Document in migration guide. |
| AGENTS.md template's rendered length exceeds 150 lines after variable expansion | Medium | Test with maximum-config `samuel.toml`. CI check uses rendered output, not template source. |
| Hooks framework defined here but bodies arrive in Milestone 4 | Low | Wire as no-ops; document in code; Milestone 4 fills in. |

## Open questions

- **Symlink vs copy** for `.samuel/builtins/`: symlink to `~/.samuel/builtins/` saves disk but breaks if user moves project. Copy is simpler. Recommend copy.
- **Auto-init detection**: should `samuel doctor` auto-suggest `samuel init` when run in an uninitialized project? Yes — actionable error per [[concepts/structured-errors]].
- **Project name validation**: should `samuel init my-project` reject names with slashes / spaces? Match Go module name rules.

## Task hints

1. Define `Plugin` interface + `Manifest` struct types
2. Define `DetectResult`, `InstallResult`, `UninstallResult`, `HealthStatus`, `Mutation`, `MutationKind`
3. Port `Orchestrator` struct + `Install`/`Uninstall`/`Doctor` methods
4. Port rollback context separation + `errors.Join` for uninstall
5. Build `samuel.lock` reader/writer (TOML)
6. Build embedded `fs.FS` source for built-in skills
7. Port `SamuelComponent` with content-hash idempotency
8. Port atomic-swap pattern (sibling tmp + rename + backup-restore)
9. Port path-traversal defense (`filepath.IsLocal`)
10. Port `sync.go` from v1, AGENTS.md-only
11. Update autogen marker to `<!-- Auto-generated by Samuel`
12. Define hook stubs in sync package (`sync.before`/`analyze-folder`/`write-agents-md`/`after`)
13. Configurable maps via `samuel.toml [sync.*]`
14. `samuel init` command + `parseInitFlags`
15. Samuel-repo detection (refuse to init inside Samuel)
16. Build `samuel.toml` writer with proper defaults
17. `samuel doctor` command + per-component health rendering
18. `samuel doctor --fix` re-runs Install on Detect=false
19. `samuel sync` command + dry-run + force
20. Smart bare invocation for init / sync / doctor
21. AGENTS.md template with `samuel.toml`-driven variable rendering
22. AGENTS.md ≤150 line CI check on rendered output
23. Tests: fake plugin, orchestrator rollback, sync fixtures
24. Tag `v2.0.0-alpha.2` and smoke-test on clean macOS/Linux
