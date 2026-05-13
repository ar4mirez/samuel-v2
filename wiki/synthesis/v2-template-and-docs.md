---
title: v2 Template + Docs (proposed)
type: synthesis
created: 2026-05-12
updated: 2026-05-12
sources: [2026-05-12-v1-template-docs]
tags: [v2, v2-decision]
---

# v2 Template + Docs

How v1's bloated 474-line CLAUDE.md template + duplicated docs/skills trees become v2's lean AGENTS.md + dynamic docs.

## Template — shrink from 474 → ~100 lines

v1's CLAUDE.md is a swiss-army knife loaded into every agent context. v2 splits responsibilities:

| v1 CLAUDE.md section | v2 destination |
|---|---|
| Operations (setup commands per language) | Plugin-driven. Each language plugin contributes its own snippet to AGENTS.md at sync time. |
| Boundaries | Stays in AGENTS.md. Configurable in `samuel.toml`. |
| Quick Reference | Stays in AGENTS.md (compact form). |
| Skills table (auto-generated) | Stays in AGENTS.md as `<!-- SAMUEL_PLUGINS_START -->` block. Dynamic from installed plugins. |
| Language guide links | Drops. Plugins auto-load per `metadata.extensions`. |
| Framework guide links | Drops. Same as above. |
| Core Guardrails (35+ rules) | Sourced from `samuel.toml [guardrails]`. Default ruleset shipped with framework. Rendered into AGENTS.md. |
| 4D Methodology | Stays. Compact form (~30 lines). See [[concepts/4d-methodology]]. |
| SDLC stages | Compact form or move to docs. |
| Per-folder CLAUDE.md docs | Trim to one line + link to docs. |
| Project Context (fillable) | Stays. Good UX. |
| Anti-patterns | Move to docs. Link from AGENTS.md. |
| When Stuck | Move to docs. Link from AGENTS.md. |
| Embedded changelog | Drops. Lives in docs. |

Target: AGENTS.md at ~100 lines.

**Token savings**: ~5K tokens per agent invocation. Compounds across pilot-mode iterations.

## v2 template layout

```
samuel/template/
├── AGENTS.md.tmpl                  # ~100 lines, Go template
├── samuel.toml.tmpl                # initial config
└── .samuel/
    ├── README.md                   # explain the dir
    └── (other framework runtime files written on init)
```

No CLAUDE.md by default. If user installs `claude-translator`, that plugin emits CLAUDE.md as a verbatim copy of AGENTS.md (per [[concepts/agents-md-primary]]).

## Marker pattern survives

```markdown
<!-- SAMUEL_PLUGINS_START -->
## Installed Plugins

| Plugin | Description |
|---|---|
| go-guide       | Go language guardrails |
| react          | React 18+ framework guardrails |
| create-rfd     | RFD creation workflow |
...
<!-- SAMUEL_PLUGINS_END -->

<!-- SAMUEL_GUARDRAILS_START -->
## Core Guardrails

- No function exceeds 50 lines (from samuel.toml [guardrails])
...
<!-- SAMUEL_GUARDRAILS_END -->
```

Same `<!-- ... _START/_END -->` convention as v1. Same user-customization preservation rule (content between markers can be rewritten; everything else is user-owned).

## Docs site — keep the stack, change the structure

### Keep

- **mkdocs-material**. Industry standard. No reason to swap.
- **Light/dark palette, instant navigation, code copy.** All useful.
- **`edit_uri`** for "edit this page on GitHub" links.
- **GitHub Pages deployment** (`https://ar4mirez.github.io/samuel/`).

### Change

| v1 docs/ | v2 docs/ |
|---|---|
| `core/` (overview, claude-md, methodology, guardrails, agent-directory) | `core/` — same purpose, but `claude-md.md` → `agents-md.md`. Add `plugins.md` and `4d-methodology.md`. |
| `getting-started/` (installation, quick-start, first-task, migration-v3) | `getting-started/` — drop `migration-v3.md`. Add `migration-v1.md` (the v1 deprecation notice — not an upgrade guide, just "here's what changed"). |
| `languages/` (22 files) | Drops as duplicates. Generated from installed-plugin manifests if plugins ship with docs. |
| `frameworks/` (34 files) | Same — drops as duplicates. |
| `workflows/` (25 files) | Same — drops as duplicates. |
| `reference/` (cli, faq, contributing, cross-tool, changelog) | Same. Update content for v2. |
| `rfd/` (RFDs) | Same. New RFDs cover v2 design decisions. |

### New v2 docs sections

- `docs/concepts/` — port the wiki's concept pages (extensibility-design, plugin-format, versioning, etc.) into user-facing form.
- `docs/plugins/` — auto-generated index of registry plugins. Pulls from `samuel-registry/index.toml`. One page per plugin if the plugin ships docs.
- `docs/plugin-authors/` — guide to writing v2 plugins (manifest, hooks, capability model, TinyGo WASM tooling).

### Drop the three-way duplication

v1: `internal/skills/content/<name>/SKILL.md` ↔ `.claude/skills/<name>/SKILL.md` ↔ `docs/<category>/<name>.md`.

v2: each plugin lives in its own repo. The plugin repo carries the SKILL.md. The framework docs site doesn't duplicate plugin content — it links out to the plugin's own README/docs (rendered in `docs/plugins/<name>.md` if available).

Single source of truth per plugin.

## Migration impact

For someone running v1 today:

1. Their `template/CLAUDE.md` becomes obsolete. v2 emits AGENTS.md.
2. Their `.claude/skills/<name>/` is preserved per [[synthesis/v2-skill-migration-plan]] (each skill becomes a plugin).
3. Their `.claude/auto/prd.json` reads unchanged in v2 (same schema).
4. Their `samuel.yaml` becomes `samuel.toml` after a manual translation. (Or: ship a one-time `samuel migrate-config` helper — but per clean-break decision, probably not.)

## Open

- **Where do the v2 docs get hosted?** Same domain (`ar4mirez.github.io/samuel/`)? Or move to `samuel.dev` (which is referenced in v1 error DocsURL fields)? Suggest: register `samuel.dev`, redirect from the GitHub Pages URL.
- **Plugin docs aggregation**. If 50 plugins each ship their own docs site, browsing them feels fragmented. The `docs/plugins/<name>.md` page pulls the plugin's README and renders it inline as a workaround. Better long-term: a plugin-author convention for "samuel-docs.md" alongside SKILL.md.
- **Versioned docs**. mkdocs-material supports versioning via mike. Worth adding for v2 (so users see docs for the version they installed).
