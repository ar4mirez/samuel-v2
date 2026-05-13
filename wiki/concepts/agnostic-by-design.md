---
title: Agnostic by design (the cross-tool invariant)
type: concept
created: 2026-05-12
updated: 2026-05-13
sources: []
tags: [v2, v2-decision, invariant]
---

# Agnostic by design

> **Post-launch update (2026-05-13).** Most of this page captures the pre-launch design intent. Shipped reality differs in one scoped way: **the `AGENTS.md → CLAUDE.md` mirror ships built-in** (`internal/translator/claude/`), introduced in v2.0.0-rc.4 after manual testing showed every other major coding assistant (Codex, Aider, Gemini, Cline, Cursor) reads AGENTS.md natively while Claude Code does not. Requiring every Samuel user to install a plugin for the trivial mirror was friction without payoff. Every richer translator surface (`.cursor/rules/`, `.codex/*`, future tools) still lives in plugins. The `agnostic-check` CI gate was narrowed accordingly: `CLAUDE.md` is now an allowed string in framework code; `.claude/`, `.cursor/`, `.codex/`, `Cursor`, `Codex CLI` remain forbidden. See [[synthesis/v2-rc-cycle-lessons]] for the decision rationale.

v2 is a framework, not a Claude wrapper. **Nothing in the framework binary or default-installed plugins assumes a specific AI coding assistant — except for the deliberate, scoped Claude carve-out documented in the callout above.** This page captures the invariant that the rest of Samuel's framework code must not violate.

## The invariant (as of v2.0.0-rc.4+)

A user installing Samuel without any translator plugins gets:

- A `CLAUDE.md` mirror of `AGENTS.md` (built-in Claude translator; can be disabled with `[translators.claude] enabled = false`).
- **Nothing else tool-specific.** No `.claude/` directory. No `.cursor/rules/`. No `.codex/*`. No Anthropic-specific endpoint. No Anthropic env var.

Any agent-specific behavior comes from a **translator plugin** installed on demand:
- `claude-translator` — emits CLAUDE.md, writes `.claude/settings.json` hooks
- `codex-translator` — emits Codex-specific files
- `cursor-translator` — emits `.cursor/rules/*.md`
- `continue-translator` — emits Continue rules
- (any future LLM tool) — its own translator plugin

The framework has no opinion on which one(s) you install. The framework's own outputs are all in `AGENTS.md` (cross-tool canonical) and `.samuel/` (framework's namespace).

## What the framework writes by default

| File | Why agnostic |
|---|---|
| `AGENTS.md` (root + per-folder) | Cross-tool canonical per [agents.md](https://agents.md) standard |
| `samuel.toml` | TOML, no agent in path |
| `samuel.lock` | TOML, no agent in path |
| `.samuel/run/prd.json` | Samuel's namespace, not `.claude/` |
| `.samuel/run/progress.md` | Same |
| `.samuel/run/task-context.md` | Same |
| `.samuel/run/progress-context.md` | Same |
| `.samuel/run/project-snapshot.md` | Same |
| `.samuel/builtins/skills/*` | Framework-bundled skills, in Samuel's namespace |

What the framework does **NOT** write by default:

- `CLAUDE.md` — only when `claude-translator` plugin is installed
- `.claude/settings.json` — same
- `.cursor/rules/*` — only when `cursor-translator` plugin is installed
- `.codex/*` — only when `codex-translator` plugin is installed
- Any tool-specific env file (`.env.claude`, `.env.openai`)

## What the framework binary references

The Go code does not import or shell out to any specific AI tool. The agent invocation layer:

1. Reads `[methodology.<name>] agent = "..."` from `samuel.toml` (default `claude` because most users will want it; user can change).
2. Looks up the **agent adapter plugin** for that name.
3. Calls the plugin's standard `invoke(prompt)` interface.
4. The plugin handles per-tool flags, env vars, prompt translation.

No `if aiTool == "claude"` switch in core code. That switch lives inside each adapter plugin.

## Built-in agent adapters (still agnostic)

v1 ships built-in support for **five** agents through one interface ([[concepts/multi-agent-support]]):

- Claude
- Codex
- Copilot
- Gemini
- Kiro

v2 keeps this. The five adapters live in the framework binary as built-in modules — but they're behind the **same adapter interface**, no privileged status. Plugin authors can add `aider`, `opencode`, anything else, and the framework calls them the same way.

The default in `samuel.toml` is `agent = "claude"`. The user can change it.

## The `samuel.toml` is the only place agent choice surfaces

```toml
[methodology.ralph]
agent = "claude"             # or "codex", "copilot", "gemini", "kiro", or any installed adapter plugin name
```

Everything downstream of this read is agent-agnostic. The framework dispatches via adapter; the adapter does the per-tool work.

## Sandbox is agnostic

The OCI sandbox image used to run the agent is declared by the **agent adapter plugin's manifest**, not in framework code:

```toml
# samuel-claude-runner plugin manifest
[oci]
image = "ghcr.io/ar4mirez/samuel-claude-runner:1.0.0"
```

If you swap to `codex-runner`, you get a different image with Codex installed. Framework doesn't care.

See [[entities/docker-sandbox]] for the v1 implementation that already does this for five agents.

## Per-folder context is agnostic

[[concepts/per-folder-context]] writes only AGENTS.md. The `claude-translator` plugin (if installed) mirrors to CLAUDE.md. The framework's generator doesn't even know about CLAUDE.md.

## Methodology prompts are agnostic

Auto-mode prompt templates ([[entities/auto-prompts]]) say "read task-context.md, implement the task, run quality checks, commit." Generic to any agent with file-read + bash + commit tooling. No `<claude>` tags. No `Claude:` prefix. The agent adapter handles per-tool prompt formatting at invocation time.

## What v2 ships without any translator plugin

User runs:

```
samuel install
samuel init
samuel install go-guide
samuel run init
samuel run start
```

Result:
- `samuel.toml` written.
- `AGENTS.md` written (root + per-folder).
- `.samuel/run/` populated.
- `.samuel/builtins/skills/go-guide/SKILL.md` mounted.
- Loop runs against the configured agent (default Claude via built-in adapter, or whatever the user set).

No `CLAUDE.md` exists. No `.claude/` directory exists. Codex user is just as well-served as Claude user.

## CI invariant check

A check in v2's release CI verifies the invariant:

```yaml
- name: Agnostic-by-design check
  run: |
    # Framework binary must not write CLAUDE.md
    grep -r '"CLAUDE\.md"' samuel/internal/ && \
      { echo "::error::Framework references CLAUDE.md by literal — should be in claude-translator plugin"; exit 1; }
    # Framework binary must not write .claude/ paths
    grep -r '"\.claude/' samuel/internal/ && \
      { echo "::error::Framework writes to .claude/ — should be in claude-translator plugin"; exit 1; }
    echo "✓ Agnostic invariant holds"
```

Plugins are exempt — they're the right place for tool-specific paths.

## Audit findings from initial wiki lint

The agnostic invariant was at risk in three places during the wiki's exploration. All fixed:

1. **`concepts/prompt-template-variables.md`** had a `ClaudeMD string` field in `PathsInfo`. Fixed: removed; tool-specific paths come from translator plugin namespaces.
2. **`synthesis/v2-command-tree.md`** described `samuel sync` as "the per-folder CLAUDE.md generator". Fixed: AGENTS.md generator.
3. **`concepts/per-folder-context.md`** title was "auto-generated CLAUDE.md / AGENTS.md". Fixed: AGENTS.md only.

The framework's own runtime files (`.samuel/run/*`) and the framework binary's outputs were already agnostic — no fixes needed there.

## v2 decision #v2-decision

- **Agnostic-by-design is a hard invariant.** Framework code, default-installed plugins, prompt templates, runtime files: all agent-agnostic. CI enforces.
- **Tool-specific behavior is plugin territory.** Each agent gets a translator plugin (`claude-translator`, `codex-translator`, etc.). They are symmetric in status — none privileged.
- **The default `agent = "claude"`** in `samuel.toml` is a default, not lock-in. User can change it without reinstalling anything.

## Open

- **Should the framework ship `claude-translator` in the starter pack** so most users get CLAUDE.md without explicit install? Tempting (better default UX) but slippery (re-privileges Claude). Recommend: **no.** The starter pack is methodology workflows (create-rfd, etc.), not tool translators. Users opt in to tool translators per their tool.
- **Discoverability**: `samuel doctor` could detect installed AI tools on PATH (claude, cursor, codex CLIs) and suggest installing the matching translator plugin. Helpful nudge, not auto-install.
- **`AGENTS.md` standard evolution**: the [agents.md](https://agents.md) standard is young. If a successor format emerges that's more canonical, v2 follows. Translator plugins absorb the disruption.

## Related

- [[concepts/agents-md-primary]] — AGENTS.md primary decision
- [[concepts/multi-agent-support]] — five built-in adapters, plugin-extensible
- [[entities/docker-sandbox]] — v1 already runs five agents through one sandbox interface
- [[synthesis/positioning-rails-for-coding-assistants]] — "Rails for coding assistants" plural by design
