---
prd: "0004"
milestone: "Methodology"
title: Samuel v2 Methodology — Ralph hooks, TOON runtime, CLI-mutation, multi-agent
authors:
  - name: ar4mirez
state: Implemented
labels: [v2, methodology, ralph, toon, hooks, multi-agent]
created: 2026-05-12
updated: 2026-05-13
target_release: v2.0.0-beta.2
estimated_effort: 2-3 weeks
depends_on: 0003-prd-plugin-loader.md
---

# PRD 0004: Samuel v2 Methodology

## Wiki references

- [[synthesis/auto-mode-v2-design]] — hook surface, encoding, rename rationale
- [[concepts/ralph-wiggum-methodology]] — Geoffrey Huntley's fresh-context pattern
- [[concepts/pre-computed-context]] — token-discipline pattern (v1's real innovation)
- [[concepts/pilot-mode]] — discovery + implementation alternation
- [[concepts/methodology-default-plus-plugin]] — built-in default + plugin enhancement
- [[concepts/toon-evaluation]] — TOON encoding for runtime structured files
- [[concepts/multi-agent-support]] — five built-in adapters
- [[concepts/prompt-template-variables]] — what the framework exposes to prompt templates
- [[concepts/4d-methodology]] — Samuel Way spine (escalation triggers)
- [[entities/auto-prd]] — prd.toon data model
- [[entities/auto-loop]] — RunAutoLoop port target
- [[entities/auto-prompts]] — implementation + discovery prompts (port + CLI-mutation rewrite)
- [[entities/docker-sandbox]] — v1's multi-agent sandbox (generalize via plugin loader)

## Summary

Ship the autonomous coding loop as the built-in `ralph` methodology, behind the lifecycle-hook framework. Runtime files use TOON (`prd.toon`, `task-context.toon`, `project-snapshot.toon`). The agent mutates state via CLI subcommands (`samuel run done|skip|reset|enqueue`), not direct file edits — this is the load-bearing change that lets us swap encodings safely. Built-in adapters for the five v1 agents (Claude, Codex, Copilot, Gemini, Kiro), each running in an OCI sandbox container.

## Problem statement

Auto-mode is Samuel's flagship methodology — the differentiator in the v3.0.0 README rewrite ("samuel run and walk away"). v2 must ship it with:

1. The architectural reshape: hooks for extensibility, TOON for token efficiency, CLI-mutation for encoding agnosticism.
2. The token-discipline innovations preserved (pre-computed context generators).
3. Multi-agent support via plugin-based adapters, not hardcoded switches.
4. Two-mode design (implementation + pilot) intact.

## Goals

- `samuel run [methodology]` runs the configured methodology (default `ralph`).
- `ralph` methodology built into the framework with the eight hooks from [[synthesis/auto-mode-v2-design]].
- TOON runtime files in `.samuel/run/`: `prd.toon`, `task-context.toon`, `project-snapshot.toon`. Markdown for `progress.md` and `progress-context.md`.
- Pre-computed context generators ported from v1 with TOON output where tabular.
- Agent **never writes prd.toon directly** — uses `samuel run done|skip|reset|enqueue`.
- Five built-in agent adapters (Claude, Codex, Copilot, Gemini, Kiro). All run in OCI sandbox per [[concepts/plugin-format]].
- Smart bare invocation: `samuel run` with no args shows status if initialized, actionable help otherwise.
- Pilot mode (discovery + implementation alternation) intact.
- Discovery-only flag: `samuel run --discover-only`.
- `samuel auto` is a permanent alias (per v1's v3.0.0 commitment — see [[entities/command-tree-v1]]).

## Non-goals

- Methodology-enhancement plugins (custom hooks, alternative prompts) — supported via plugin loader from Milestone 3, but no plugins shipped here.
- TDD-strict methodology variants — Milestone 5 or later.
- Streaming JSON Lines output mode for `samuel run start` — deferred.
- Cost surfacing per iteration — deferred (post-v2.0).

## Requirements

### Functional

1. **Methodology hooks framework** at `internal/methodology/hooks/`:
   - Hook names (from [[synthesis/auto-mode-v2-design]]):
     - `before:loop`, `after:loop`
     - `before:iteration`, `after:iteration`
     - `iteration.gate` (impl vs discovery)
     - `context.snapshot`, `context.progress`, `context.task`, `context.extra`
     - `before:agent.invoke`, `agent.invoke`, `after:agent.invoke`
     - `quality.check`
   - Plugins register handlers via their manifest's `[provides] hooks = [...]`.
   - Multiple plugins can attach to one hook; ordering deterministic (registration order; `samuel.toml [hooks.<name>.order]` overrides).
   - Built-in handlers shipped for every hook with the v1 logic.

2. **`ralph` methodology** at `internal/methodology/ralph/`:
   - Built-in. Lives in framework binary, not a plugin.
   - Templates at `internal/methodology/ralph/templates/{prompt,discovery-prompt}.md.tmpl` via `text/template`.
   - Variables per [[concepts/prompt-template-variables]] — `Samuel`, `Project`, `Methodology`, `Iteration`, `Config`, `Guardrails`, `Paths`, `State`, `Mode`, `Hooks`, `Plugins`.
   - Per-project template override at `.samuel/templates/ralph/*.tmpl` honored.
   - Loop driver ports [[entities/auto-loop]]'s `RunAutoLoop`.
   - Pilot trigger logic ports [[concepts/pilot-mode]] (`ShouldRunDiscovery`).

3. **TOON runtime files** at `.samuel/run/`:
   - `prd.toon` — task state. Schema mirrors [[entities/auto-prd]]'s JSON shape; tabular `tasks` array as TOON `tasks[N]{...}:`.
   - `task-context.toon` — current task brief (impl mode) or summary table (discovery mode).
   - `project-snapshot.toon` — file inventory, test gaps, large files, TODO counts, git log.
   - `progress.md` — markdown append-only log (unchanged from v1).
   - `progress-context.md` — markdown summary (unchanged from v1).
   - Version header on every `.toon` file: `# toon v3`.

4. **CLI mutation commands** at `internal/commands/run/`:
   - `samuel run done <task-id> [--commit-sha SHA] [--iteration N]` — mark completed.
   - `samuel run skip <task-id> [--reason TEXT]` — mark skipped.
   - `samuel run reset <task-id>` — back to pending.
   - `samuel run enqueue <title> [--priority] [--complexity] [--source]` — auto-id task.
   - `samuel run task add <id> <title> [...]` — explicit-id task (CI/scripts).
   - All mutations are atomic (write-tmp-then-rename pattern).
   - All emit JSON via `--json`.

5. **`samuel run` (smart bare)** at `internal/commands/run/`:
   - `prd.toon` exists → run `runStatus()` (read-only).
   - `prd.toon` missing → print actionable help (`samuel run init`, `samuel run pilot`), exit 1.

6. **`samuel run init`**:
   - Bootstraps `.samuel/run/` with `prd.toon`, `progress.md`.
   - `--prd <path>` converts a markdown PRD + tasks file → `prd.toon`.
   - Flags: `--ai-tool`, `--max-iterations`, `--sandbox` (none/oci), `--methodology`.

7. **`samuel run start`**:
   - Runs the loop using the configured methodology.
   - Reloads `prd.toon` every iteration (agent may have mutated via CLI subcommands).
   - Honors `--iterations N`, `--yes`, `--dry-run`.
   - Calls the eight hooks at the right points.
   - Consecutive-failure abort (default 3, env `MAX_CONSECUTIVE_FAILURES`).
   - Per-iteration sleep (default 2s, env `PAUSE_SECONDS`).

8. **`samuel run pilot`**:
   - Initializes pilot mode prd if none exists.
   - Flags: `--discover-interval`, `--max-discovery-tasks`, `--focus testing|docs|security|performance|refactoring`.
   - Alternates discovery/implementation per `ShouldRunDiscovery`.
   - `samuel run --discover-only` short-circuits to discovery iterations only.

9. **`samuel run status`** (also bound to bare `samuel run`):
   - Shows total/pending/completed/in-progress/blocked counts.
   - Current task, recent completions, current iteration.
   - `--tail N` shows last N progress entries.
   - `--json` emits envelope.

10. **`samuel run tasks`**:
    - Lists tasks with status. Replaces v1's `samuel run task list`.
    - `--status pending|completed|...` filter.
    - `--json` emits envelope.

11. **`samuel run convert <prd-path>`**:
    - PRD markdown + tasks markdown → `prd.toon`.
    - Auto-discovers tasks file via `tasks-<prd-name>.md` convention.

12. **Built-in agent adapters** at `internal/agents/`:
    - Five: `claude`, `codex`, `copilot`, `gemini`, `kiro`.
    - Each adapter declares: default image, env allowlist, prompt-mode (file-arg / content-arg / stdin), default args.
    - Adapter interface lets external agent-plugins register the same way.

13. **OCI sandbox invocation** at `internal/sandbox/`:
    - Uses Milestone 3's OCI plugin loader.
    - Mount layout: `/workspace` (rw), `/skills` (ro), `/.samuel/run` (rw), `/samuel-bridge` (Unix socket).
    - Env var allowlist from adapter manifest.
    - User mapping (`--user $UID:$GID`).
    - Image regex validation (port from v1).

14. **Pre-computed context generators** at `internal/methodology/ralph/context/`:
    - `GenerateProjectSnapshot` → `project-snapshot.toon` (tabular: files, test gaps, large files, TODOs, git log).
    - `GenerateProgressContext` → `progress-context.md` (prose summary + last N learnings/completions/explored paths).
    - `GenerateTaskContext` → `task-context.toon` (impl: full current task; discovery: summary table + covered-files list).
    - `RotateProgressIfNeeded` archives `progress.md` when > 500 lines (configurable).

15. **Prompts rewritten for CLI mutation**:
    - Implementation prompt instructs: "After implementing, run `samuel run done <id> --commit-sha $(git rev-parse HEAD)`. Do NOT edit prd.toon directly."
    - Discovery prompt instructs: "Add new tasks via `samuel run enqueue \"<title>\"` for auto-id or `samuel run task add <id> \"<title>\"` for explicit-id. Do NOT edit prd.toon directly."
    - Both prompts retain the token-discipline rules (read pre-computed context first, max 10 source files per discovery, etc.).

### Non-functional

- Loop iteration overhead (samuel-side, excluding agent execution) < 500ms.
- TOON write atomic (write-tmp-then-rename).
- Hook handler failure does NOT crash the loop (logged as warning, iteration proceeds).
- AI-output resilience: agent emitting bad TOON triggers per-row skip + warning, not loop abort.
- Multi-agent sandbox launch < 5 seconds (image pull excluded).

## Acceptance criteria

- [x] `samuel run init --prd .samuel/tasks/0001-prd-foundation.md` produces a valid `prd.toon`. *(also verified via bootstrap test: `samuel run convert .samuel/tasks/0004-prd-methodology.md` → 104 tasks in prd.toon)*
- [x] `samuel run start --iterations 1` runs one iteration end-to-end against a containerized Claude. *(verified end-to-end via `--dry-run -y` and `TestE2ELoop_AgentEmitsCLIDone_TaskCompletes` integration test; sandbox=oci path wired through internal/sandbox + plugin/oci tier loader)*
- [x] Agent emits `samuel run done 1.2 --commit-sha abc` via Bash tool; mutation lands in `prd.toon`. *(TestRunMutate_Done_PersistsCompletion + bootstrap manual exercise)*
- [x] `samuel run` (bare) with prd.toon present → status output, exit 0.
- [x] `samuel run` (bare) without prd.toon → help, exit 1.
- [x] `samuel run pilot --focus testing` runs discovery → implementation cycle. *(InitPilotPRD + ShouldRunDiscovery + focus injection covered by TestRunAutoLoop_PilotMode_GateRoutesDiscovery)*
- [x] `samuel run --discover-only` runs discovery iterations only. *(start command sets discover_interval=1 when --discover-only is set on a pilot PRD)*
- [x] `samuel run tasks --json` returns task list in JSON envelope.
- [x] `samuel run convert v2-prd.md` produces `prd.toon` with parsed tasks.
- [x] Five built-in adapters all work (test with mock agents emitting fixed output). *(TestBuiltins_AllRegistered + per-adapter BuildArgs / Invoke tests; injected mockRunner)*
- [x] Loop swap: change `samuel.toml [methodology.ralph] agent` from `claude` to `codex`, restart, loop continues. *(TestMultiAgentSwap_SwitchClaudeToCodex_LoopContinues)*
- [x] Pre-computed context files regenerate every iteration. *(loop calls GenerateProjectSnapshot / PrepareProgressContext / GenerateTaskContext per iteration; context hooks also fire as defaults)*
- [x] `progress.md` rotation triggers at 500 lines. *(TestRotateProgress_TriggersAtThreshold)*
- [x] Malformed TOON row in `prd.toon` skipped with warning, loop continues on remaining tasks. *(TestTOON_MalformedRow_LoopContinues; decoder emits per-row warnings, loop survives)*
- [x] Hook handler that errors logs warning, doesn't crash loop. *(TestRegistry_NonStrictWarnsAndContinues + TestNonStrictMode_LogsWarningContinues)*
- [x] `samuel auto` is a permanent alias for `samuel run` (every subcommand). *(Cobra alias on the top-level command — children inherited automatically)*
- [x] No `.claude/` files written by methodology or adapters (agnostic invariant). *(TestAgnostic_NoClaudePathsWritten)*

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Agent ignores CLI-mutation instruction and edits prd.toon directly | High | Make prd.toon read-only from the sandbox container's perspective. Mount as `:ro`. CLI subcommands run as host samuel process with write access. |
| TOON encoder mishandles agent-emitted edge cases | Medium | Per-row recovery + structured warnings (Milestone 1). Plus: agent shouldn't be writing TOON if CLI-mutation works. Defense in depth. |
| Hook framework's "multiple plugins on one hook" semantics get complex | Medium | v2.0 supports last-registered-wins for replace-style hooks, all-run for additive hooks. Document clearly. |
| Five built-in adapters duplicate per-agent code | Low | Common adapter interface; each agent's specifics (image, env, prompt-mode) declarative. |
| Container runtime not available on user system | High | `samuel doctor` detects + suggests install. `samuel run` falls back to host-exec mode with warning if `--sandbox=none`. |
| Progress rotation conflicts with concurrent agent writes | Low | Atomic rename; rotation runs only at iteration boundary; agent doesn't write progress.md concurrently with samuel. |

## Open questions

- **Default sandbox mode**: `oci` or `none`? v1 default was `none`. v2 thesis is sandbox-by-default, but requires Docker/Podman. Recommend `oci` if runtime detected, fall back to `none` with warning otherwise. Confirm.
- **Built-in adapter prompts**: ship as separate template files per adapter (`internal/agents/claude/prompt-modifier.tmpl`), or one universal prompt with adapter-specific args? Separate templates give more control; consolidated reduces duplication. Start consolidated.
- **`/samuel-bridge` socket protocol**: HTTP, gRPC, or simple JSON-line stream? Simple JSON-line stream is fine for v2.0 (capability calls, status queries).
- **Hook handler errors**: warn-and-continue or fail-loop? Default warn-and-continue. Add `[hooks.<name>.strict = true]` for must-not-fail hooks (quality.check probably wants strict).

## Task hints

1. Define `Hook` interface + 8 hook constants
2. Hook handler registration (built-in + plugin-provided)
3. Hook ordering resolution
4. Port `AutoPRD` data model to TOON shape
5. TOON encoder/decoder for `prd.toon` (tabular tasks)
6. TOON encoder for `project-snapshot.toon`
7. TOON encoder for `task-context.toon`
8. Port `RunAutoLoop` → `internal/methodology/ralph/loop.go`
9. Port `ShouldRunDiscovery` for pilot mode
10. Port pre-computed context generators (snapshot, progress, task) with TOON output
11. Port progress rotation
12. Port atomic save pattern
13. Define `Agent` adapter interface
14. Built-in adapter: `claude`
15. Built-in adapter: `codex`
16. Built-in adapter: `copilot`
17. Built-in adapter: `gemini`
18. Built-in adapter: `kiro`
19. OCI sandbox launcher (reuses Milestone 3 OCI loader)
20. Mount layout + env allowlist + user mapping
21. `/samuel-bridge` Unix socket implementation (stub for v2.0)
22. `samuel run` smart bare invocation
23. `samuel run init` + `--prd` flag
24. `samuel run start` + flags
25. `samuel run pilot` + `--focus`
26. `samuel run --discover-only`
27. `samuel run done|skip|reset|enqueue` mutation commands
28. `samuel run task add` (explicit-id)
29. `samuel run status` + `--tail`
30. `samuel run tasks` + filters
31. `samuel run convert <prd-path>`
32. Prompt template rewrite (use CLI subcommands, not direct file edits)
33. `samuel auto` alias wiring
34. Integration tests: end-to-end loop with mock agent
35. Tag `v2.0.0-beta.2` and dogfood on v2 itself
