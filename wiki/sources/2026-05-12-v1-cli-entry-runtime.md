---
title: v1 CLI entry + .claude runtime
type: source
created: 2026-05-12
updated: 2026-05-12
sources: []
tags: [v1, cli, runtime]
---

# v1 CLI Entry + `.claude/` Runtime

Ingest pass 7. The Go binary entry point + the `.claude/` runtime directory that ships in every initialized project.

## Files

- `samuel_v1/cmd/samuel/main.go` (18 lines) — binary entry point
- `samuel_v1/cmd/CLAUDE.md` (4 lines) — auto-generated folder stub
- `samuel_v1/.claude/settings.json` (15 lines) — Claude Code settings with hook registration
- `samuel_v1/.claude/hooks/check-gstack.sh` (20 lines) — bash hook script
- `samuel_v1/.claude/auto/{prd.json, progress.md, prompt.md, discovery-prompt.md}` — auto-mode runtime artifacts (dogfood)

## Key claims

### `cmd/samuel/main.go`

```go
func main() {
    if err := commands.Execute(); err != nil {
        red := color.New(color.FgRed).SprintFunc()
        fmt.Fprintf(os.Stderr, "%s %s\n", red("Error:"), err.Error())
        os.Exit(1)
    }
}
```

That's the entire entry point. 18 lines. All logic lives in `internal/commands`. Standard Go layout (`cmd/<binary>/main.go`).

Exit code 1 on any error. Colored "Error:" prefix to stderr. Clean.

### `.claude/settings.json` — Claude Code hook registration

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Skill",
        "hooks": [
          {
            "type": "command",
            "command": "\"$CLAUDE_PROJECT_DIR/.claude/hooks/check-gstack.sh\""
          }
        ]
      }
    ]
  }
}
```

**This is Claude Code's feature, not Samuel's.** Claude Code lets users register `PreToolUse` hooks that run before the agent uses a specific tool. The hook is a shell command; its stdout JSON controls whether the tool call proceeds.

v1 hijacks this to gate Skill usage on gstack presence.

### `.claude/hooks/check-gstack.sh`

```bash
#!/bin/bash
if [ ! -d "$HOME/.claude/skills/gstack/bin" ]; then
  cat >&2 <<'MSG'
BLOCKED: gstack is not installed globally.

gstack is required for AI-assisted work in this repo.

Install it:
  git clone --depth 1 https://github.com/garrytan/gstack.git ~/.claude/skills/gstack
  cd ~/.claude/skills/gstack && ./setup --team

Then restart your AI coding tool.
MSG
  echo '{"permissionDecision":"deny","message":"gstack is required but not installed. See stderr for install instructions."}'
  exit 0
fi

echo '{}'
```

- Checks for `~/.claude/skills/gstack/bin/`.
- If missing, prints install instructions to stderr + outputs JSON `{"permissionDecision":"deny", ...}` to stdout.
- Claude Code reads the stdout JSON and **denies the tool call** before the agent ever runs.
- Otherwise outputs `{}` (no opinion, allow).

This is how v1 enforces "gstack must be installed before any Skill works" at the agent boundary — much stronger than asking nicely in CLAUDE.md.

### `.claude/auto/` dogfood

Samuel v1's own repo has `.claude/auto/` populated with a real autonomous-loop state:

- `prd.json` — pilot mode, claude tool, 30 max iterations, Go quality checks. 60+ tasks in the array, many completed with commit SHAs. Real production use of pilot mode.
- `progress.md` — 1034 lines of append-only history.
- `prompt.md` — the implementation prompt.
- `discovery-prompt.md` — the discovery prompt.

Pilot mode found and fixed real issues:
- "Fix unchecked file.Close() errors after io.Copy in downloader.go and extractor.go" — completed with SHA `a53222d`.
- "Add Docker sandbox image name validation in auto_loop.go" — the image-validation regex in `docker.go` was added by pilot mode.

**The methodology has actually shipped value.** This is dogfood proof that the design works in practice.

## Assessment

- **Credibility**: high.
- **Quality of `cmd/samuel/main.go`**: standard Go layout, nothing to improve.
- **Insight on `.claude/settings.json`**: v1 has a working integration with Claude Code's hook system for enforcing prerequisites at the tool boundary.

## v2 implications

### `cmd/samuel/main.go` — `#rescue`

Port verbatim. Standard Go layout, no improvements needed. v2's `samuel/cmd/samuel/main.go` will be roughly identical:

```go
func main() {
    if err := commands.Execute(); err != nil {
        // (lipgloss equivalent for the red prefix)
        os.Exit(1)
    }
}
```

### `.claude/settings.json` + hooks → translator plugin concern

Per [[concepts/agents-md-primary]], tool-specific files are managed by translator plugins. The Claude Code hook system fits that model:

- A `claude-translator` plugin owns `.claude/settings.json`.
- The plugin can install Samuel-provided hooks (e.g., enforce-skill-validity, deny-out-of-workspace-reads, audit-bash-commands) at install time.
- v2 framework itself does **not** write `.claude/settings.json`. That's the plugin's filesystem.

The general pattern — **enforce prerequisites at the agent boundary** — is worth promoting to a v2 concept. See [[concepts/claude-code-hooks]].

### `check-gstack.sh` — `#drop`

gstack drops in v2 ([[entities/component-gstack-gbrain]]). The hook script goes with it.

The **pattern** (use Claude Code's PreToolUse hook to enforce framework prerequisites) is `#rescue` — see [[concepts/claude-code-hooks]].

### `.claude/auto/` runtime — `#refactor`

Per [[synthesis/auto-mode-v2-design]], the runtime dir moves to `.samuel/run/` (or `.samuel/<methodology>/`). Same files, same shape, namespaced to Samuel rather than Claude Code.

The dogfood prd.json structure is the v2 prd.json structure unchanged. Port the schema verbatim per [[entities/auto-prd]].

## Related pages

- [[concepts/claude-code-hooks]]
- [[entities/auto-runtime-files]]
- [[concepts/agents-md-primary]]
