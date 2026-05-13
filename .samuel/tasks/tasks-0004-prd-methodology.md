# Tasks — PRD 0004: Samuel v2 Methodology

> Generated from [0004-prd-methodology.md](0004-prd-methodology.md) on 2026-05-12.
> Depends on PRD 0003 (Plugin Loader) being complete.

## Relevant files

- `samuel_v1/internal/core/auto*.go` (11 files) — primary source for port
- `samuel_v1/internal/core/docker.go` — multi-agent sandbox pattern to generalize
- `samuel_v1/internal/commands/auto*.go` — CLI surface to port
- `.wiki/synthesis/auto-mode-v2-design.md`, `.wiki/concepts/methodology-default-plus-plugin.md`, `.wiki/concepts/pre-computed-context.md`, `.wiki/concepts/pilot-mode.md`, `.wiki/concepts/prompt-template-variables.md` — design references
- RFD 0004, 0006 (Committed) — design contract

## Tasks

- [x] 1.0 Hook framework [~6,000 tokens - Complex]
  - [x] 1.1 Define `HookName` constants — 13 hooks per RFD 0004 (before/after loop, iteration, gate, context.{snapshot,progress,task,extra}, before/agent.invoke/after, quality.check)
  - [x] 1.2 Define `Hook` interface + `HookInput`/`HookOutput` structs
  - [x] 1.3 Implement `Registry` struct with ordered handler chain per hook
  - [x] 1.4 Order resolution: built-in default at order 100, plugins default to 200, samuel.toml overrides
  - [x] 1.5 Per-hook `strict` config — true aborts iteration on handler error; false logs warning
  - [x] 1.6 Default strict: `quality.check` + `before:loop`; default non-strict for others
  - [x] 1.7 Per-hook timeout (default 5 min, configurable in samuel.toml)
  - [x] 1.8 Hook handler error → structured `[hooks.warning]` entry to progress.md
  - [x] 1.9 `--profile` flag emits `[hooks.timing]` entries
  - [x] 1.10 Capability check before invoking handler (per RFD 0004 resolution #6)
  - [x] 1.11 Tests: chain composition, order override, strict failure, timeout

- [x] 2.0 AutoPRD data model port [~3,000 tokens - Medium]
  - [x] 2.1 Port `AutoPRD`, `AutoProject`, `AutoConfig`, `PilotConfig`, `AutoTask`, `AutoProgress` from `samuel_v1/internal/core/auto.go`
  - [x] 2.2 Keep `AutoTask.UnmarshalJSON` numeric-ID coercion pattern (now `UnmarshalTOON` for TOON path)
  - [x] 2.3 Status / Priority / Complexity / Source / LoopStatus enums
  - [x] 2.4 `NewAutoPRD`, `NextAvailableID`, `GetNextTask`, `CompleteTask`, `SkipTask`, `ResetTask`, `AddTask`, `ValidateAutoPRD` — port from auto_tasks.go
  - [x] 2.5 Atomic save (write-tmp-then-rename)

- [x] 3.0 TOON runtime file I/O [~4,500 tokens - Medium]
  - [x] 3.1 Implement `internal/methodology/ralph/prd.go` — Load/Save for prd.toon using `internal/encoding/toon`
  - [x] 3.2 Per-row malformation recovery integrated with AutoPRD load
  - [x] 3.3 Version header on write (`# toon v3`)
  - [x] 3.4 task-context.toon writer
  - [x] 3.5 project-snapshot.toon writer
  - [x] 3.6 progress.md and progress-context.md stay markdown (unchanged)

- [x] 4.0 Pre-computed context generators (port from v1) [~5,500 tokens - Complex]
  - [x] 4.1 Port `GenerateProjectSnapshot` from `auto_snapshot.go` → `project-snapshot.toon`
  - [x] 4.2 Port file inventory walk, test gap detection, large file ranking, TODO/FIXME/HACK counts
  - [x] 4.3 Port `recentGitLog` helper
  - [x] 4.4 Port `GenerateProgressContext` from `auto_progress_context.go` → `progress-context.md` (markdown preserved)
  - [x] 4.5 Port `RotateProgressIfNeeded` (500-line threshold; configurable)
  - [x] 4.6 Port `GenerateTaskContext` from `auto_task_context.go` → `task-context.toon`
  - [x] 4.7 Discovery-mode summary table vs implementation-mode current-task detail
  - [x] 4.8 Each generator registers as default handler for its hook (`context.snapshot`, `context.progress`, `context.task`)

- [x] 5.0 Ralph loop driver [~5,500 tokens - Complex]
  - [x] 5.1 Port `RunAutoLoop` from `auto_loop.go` → `internal/methodology/ralph/loop.go`
  - [x] 5.2 Reload prd.toon every iteration (agent may have mutated via CLI)
  - [x] 5.3 Fire hooks at the 13 lifecycle points
  - [x] 5.4 `MaxConsecFails` abort (default 3, env `MAX_CONSECUTIVE_FAILURES`)
  - [x] 5.5 PauseSecs between iterations (default 2, env `PAUSE_SECONDS`)
  - [x] 5.6 `OnIterStart` / `OnIterEnd` callbacks for CLI UI

- [x] 6.0 Pilot mode [~3,500 tokens - Medium]
  - [x] 6.1 Port `PilotConfig`, `NewPilotConfig`, `ShouldRunDiscovery`, `CountPendingTasks`, `InitPilotPRD` from `auto_pilot.go`
  - [x] 6.2 Register `ShouldRunDiscovery` as default `iteration.gate` hook handler
  - [x] 6.3 Implement `--discover-only` mode (sets `discover_interval = 1`, impl iterations = 0)
  - [x] 6.4 Focus area injection (`--focus testing|docs|security|performance|refactoring`)

- [x] 7.0 Agent adapters [~5,000 tokens - Complex]
  - [x] 7.1 Define `AgentAdapter` interface at `internal/agents/adapter.go`
  - [x] 7.2 Adapter declares: default image, env allowlist, prompt-mode (`stdin-content`, `file-arg`, `content-arg`), default args
  - [x] 7.3 Built-in adapter: `claude` (`-p <content> --dangerously-skip-permissions`, env: ANTHROPIC_API_KEY)
  - [x] 7.4 Built-in adapter: `codex` (`--dangerously-bypass-approvals-and-sandbox exec <content>`, env: OPENAI_API_KEY)
  - [x] 7.5 Built-in adapter: `copilot` (port v1's behavior)
  - [x] 7.6 Built-in adapter: `gemini`
  - [x] 7.7 Built-in adapter: `kiro`
  - [x] 7.8 `Adapter.Invoke(ctx, prompt, opts)` returns output + error
  - [x] 7.9 Register `Invoke` as default `agent.invoke` hook handler

- [x] 8.0 OCI sandbox launcher [~4,000 tokens - Medium]
  - [x] 8.1 Reuse Milestone 3's OCI tier loader for image management
  - [x] 8.2 Mount layout: `/workspace` (rw), `/skills` (ro), `/.samuel/run` (**ro** — agent uses CLI subcommands), `/plugin/config` (ro), `/samuel-bridge` (gRPC socket)
  - [x] 8.3 Env var allowlist forwarding from adapter manifest
  - [x] 8.4 User mapping `--user $UID:$GID`
  - [x] 8.5 Image regex validation pre-invocation

- [x] 9.0 Prompt templates [~5,000 tokens - Complex]
  - [x] 9.1 Author `internal/methodology/ralph/templates/prompt.md.tmpl` — Go text/template, implementation prompt
  - [x] 9.2 Author `internal/methodology/ralph/templates/discovery-prompt.md.tmpl` — discovery prompt
  - [x] 9.3 **Rewrite for CLI mutation** — agent uses `samuel run done <id> --commit-sha $(git rev-parse HEAD)` not direct prd.toon edit
  - [x] 9.4 Define `PromptContext` struct per RFD 0006 with all 11 sections (Samuel, Project, Methodology, Iteration, Config, Guardrails, Paths, State, Mode, Hooks, Plugins)
  - [x] 9.5 Embed templates via `go:embed`
  - [x] 9.6 Per-project override at `.samuel/templates/ralph/*.md.tmpl` (file override beats embedded default)
  - [x] 9.7 Template helpers: `join`, `indent`, `relpath`, `hasPlugin`, `commitConvention`

- [x] 10.0 samuel run command surface [~6,500 tokens - Complex]
  - [x] 10.1 `samuel run [methodology]` — positional, default from samuel.toml `default_methodology`
  - [x] 10.2 Cobra alias `auto` for permanent v1 compat
  - [x] 10.3 Methodology name resolution: positional → alias map (`rw` → `ralph`) → config default → hardcoded `ralph` fallback
  - [x] 10.4 Smart bare invocation: status if prd.toon exists, actionable help otherwise
  - [x] 10.5 `samuel run init [--prd <path>]` — initialize runtime
  - [x] 10.6 `samuel run start [--iterations] [-y] [--dry-run] [--profile]`
  - [x] 10.7 `samuel run status [--tail N]`
  - [x] 10.8 `samuel run pilot [--focus] [--discover-interval] [--max-discovery-tasks]`
  - [x] 10.9 `samuel run --discover-only`
  - [x] 10.10 `samuel run tasks [--status pending|completed|...]`
  - [x] 10.11 `samuel run convert <prd-path>` — markdown → prd.toon

- [x] 11.0 CLI mutation commands [~3,500 tokens - Medium]
  - [x] 11.1 `samuel run done <task-id> [--commit-sha SHA] [--iteration N]` — atomic prd.toon mutation
  - [x] 11.2 `samuel run skip <task-id> [--reason TEXT]`
  - [x] 11.3 `samuel run reset <task-id>`
  - [x] 11.4 `samuel run enqueue <title> [--priority] [--complexity] [--source]` — auto-id via NextAvailableID
  - [x] 11.5 `samuel run task add <id> <title> [...]` — explicit-id for CI
  - [x] 11.6 All acquire iteration lock, atomic write, emit JSON
  - [x] 11.7 `--commit-sha` optional but documented as expected (per RFD 0006 resolution #5)

- [x] 12.0 Tests [~4,500 tokens - Medium]
  - [x] 12.1 FakePlugin for hook handler chain testing
  - [x] 12.2 Hook composition test: 2 plugins + default chain on `context.snapshot`
  - [x] 12.3 Strict-mode test: handler errors with strict=true aborts iteration
  - [x] 12.4 Strict-mode test: handler errors with strict=false logs warning, loop continues
  - [x] 12.5 End-to-end loop test with mock agent emitting fixed CLI invocations
  - [x] 12.6 TOON malformation recovery: corrupt one row in prd.toon, loop continues on remaining
  - [x] 12.7 Multi-agent test: swap adapter from claude to codex via samuel.toml, loop continues
  - [x] 12.8 Per-project template override: `.samuel/templates/ralph/prompt.md.tmpl` is used instead of embedded
  - [x] 12.9 Agnostic check: `samuel run` writes nothing to `.claude/` paths

- [x] 13.0 Tag beta.2 [~1,000 tokens - Simple]
  - [x] 13.1 Bootstrap test: convert this PRD itself to prd.toon; `samuel run start --iterations 1` works
  - [x] 13.2 CHANGELOG update
  - [x] 13.3 Tag `v2.0.0-beta.2`
