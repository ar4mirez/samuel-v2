---
title: Auto-mode prompts (implementation + discovery)
type: entity
created: 2026-05-12
updated: 2026-05-12
sources: [2026-05-12-v1-auto-mode]
tags: [v1, auto-mode, prompts]
---

# Auto-mode prompts

Two prompt templates. Both emphasize **token discipline**.

## Implementation prompt (`auto_prompt.go`)

Used in every implementation iteration. Tells the agent:

- Read `task-context.md` FIRST (not full prd.json).
- Read `progress-context.md` for prior learnings.
- Read only files listed in `files_to_modify`.
- Read `CLAUDE.md` only if needed.
- Set task to `in_progress` in prd.json.
- Implement one task. Atomic changes.
- Run all `config.quality_checks` — all must pass.
- Conventional commit: `type(scope): description`, include task ID.
- Set task to `completed`, record `commit_sha`.
- Append learnings to `progress.md`: `[timestamp] [iteration:N] [task:ID] LEARNING: ...`.

Hard rules:
- One task per iteration.
- Never skip quality checks.
- Functions ≤50 lines, files ≤300 lines.
- Tests for all new code.
- If stuck: mark task `blocked`, document reason, move on.

`GeneratePromptFile(config AutoConfig)` appends project-specific config (AITool, max iterations, paths, quality checks list) to the template.

## Discovery prompt (`auto_discovery_prompt.go`)

Used in pilot-mode discovery iterations. **Strictly no code changes.**

- Read `project-snapshot.md` FIRST. Do NOT scan tree, run find/ls, or grep for TODOs.
- Read `task-context.md` for existing task summary.
- Read `progress-context.md` for explored areas.
- Use grep over file-reads.
- **Read at most 10 source files** per discovery iteration.
- Skip files covered by pending tasks.
- Skip files in "Areas Already Analyzed" unless git log shows recent changes.

Output: append new tasks to `prd.json` with `source: "pilot-discovery"`. Atomic tasks only (≤5 files each).

Priority order: security > tests > quality > docs > perf > refactor.

`GenerateDiscoveryPrompt(config, pilot)` appends max-discovery-tasks count, optional focus area (`testing`, `docs`, `security`, `performance`, `refactoring`), and quality-checks reference.

## Where they live

In v1, both prompts are **Go string literals** in:
- `internal/core/auto_prompt.go:13-82` (GetDefaultPromptTemplate)
- `internal/core/auto_discovery_prompt.go:11-107` (GetDiscoveryPromptTemplate)

They're rendered to disk at `.claude/auto/prompt.md` and `.claude/auto/discovery-prompt.md` during init.

The runtime `.claude/auto/prompt.md` in v1's own repo (`samuel_v1/.claude/auto/prompt.md`) is a slightly older version — the Go template (auto_prompt.go) has been updated since.

## v2 implications

### `#rescue` — the patterns

- "Read THESE files FIRST, don't grep" pattern.
- Token-budget rules ("read at most N files").
- Two-mode (implementation + discovery) prompt split.
- Priority order for discovery.
- Conventional commit + task ID + learnings format.

### `#refactor` — where they live

Externalize from Go string literals to template files:

```
samuel/templates/auto/
├── prompt.md.tmpl              # Go template, rendered with AutoConfig
└── discovery-prompt.md.tmpl
```

Then:
- Built-in default: shipped in binary (via `go:embed`).
- Per-project override: `.samuel/templates/auto/prompt.md.tmpl` if present.
- Plugin override: a methodology-enhancement plugin can replace.

### `#drop`

- Hardcoded guardrails in prompt text ("functions ≤50 lines, files ≤300 lines"). Source from `samuel.toml [guardrails]` block. Render into the prompt at iteration time.

### `#open`

- Template engine — Go `text/template` is fine, no need for anything heavier.
- Variables exposed to template — at minimum: project name, AI tool, quality checks list, guardrails. Probably also: hooks registered for this iteration.
