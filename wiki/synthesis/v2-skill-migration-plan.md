---
title: v2 Skill Migration Plan
type: synthesis
created: 2026-05-12
updated: 2026-05-12
sources: [2026-05-12-v1-skill-content-survey]
tags: [v2, v2-decision, plugins, migration]
---

# v2 Skill Migration Plan

How v1's 78 skills become v2 plugins. Concrete, mechanical, scriptable.

## The four buckets

Per [[sources/2026-05-12-v1-skill-content-survey]]:

| Bucket | Count | Disposition |
|---|---|---|
| Built-in framework | 4 | Content becomes Go template strings in v2 binary |
| Starter-pack plugins | 12 | Bundled as `samuel-starter` meta-plugin, auto-installed on init |
| Pure plugins | 58 | Installable on demand from the registry |
| Drop | 2 | Replaced by built-in commands |

## Plugin packaging

Each plugin = a Git repo with this layout:

```
samuel-<plugin-name>/
├── samuel-plugin.toml          # v2 manifest (NEW)
├── SKILL.md                    # Agent Skills format (unchanged from v1)
├── scripts/                    # optional, from v1
├── references/                 # optional, from v1
└── assets/                     # optional, from v1
```

Manifest wrapper per plugin (`samuel-plugin.toml`):

```toml
name = "go-guide"
version = "1.0.0"
kind = "skill"

[samuel]
framework = "^2.0.0"

[provides]
skills = ["go-guide"]

[capabilities.filesystem]
read = ["/workspace"]

[metadata]
language = "go"
extensions = [".go"]
auto_load = true
```

The `[metadata]` block carries v1's `language` / `extensions` for auto-loading. The framework reads it; the SKILL.md frontmatter stays unchanged (Agent Skills standard compatibility per [[concepts/agent-skills-standard]]).

## Repository organization

Two options, ranked:

### Option A: One repo per plugin (recommended)

- `github.com/<org>/samuel-go-guide`
- `github.com/<org>/samuel-react`
- `github.com/<org>/samuel-create-rfd`
- ...

Pros: each plugin has its own version, issues, PRs, release cadence. Standard ecosystem pattern (npm, cargo, pip).
Cons: 78 repos to create at migration time. Manageable as a one-time scripted migration.

### Option B: Monorepo `samuel-plugins` with one folder per plugin

- `github.com/<org>/samuel-plugins/go-guide/`
- `github.com/<org>/samuel-plugins/react/`
- ...

Pros: one place to PR multiple plugins at once. Easier to keep aligned.
Cons: versioning is awkward (one tag per plugin? tags per release?). Plugins are usually independent.

**Recommendation: Option A.** Standard pattern, easier for community plugins to follow.

## Registry index

A separate repo holds the index:

```toml
# github.com/<org>/samuel-registry/index.toml
schema_version = 1

[plugin.go-guide]
repo = "github.com/<org>/samuel-go-guide"
latest = "1.0.0"
description = "Go language guardrails and patterns"
categories = ["language"]
tags = ["go", "golang"]

[plugin.react]
repo = "github.com/<org>/samuel-react"
latest = "1.0.0"
description = "React 18+ framework guardrails"
categories = ["framework"]
tags = ["react", "typescript", "jsx"]
```

- `samuel search react` reads the index, returns matching plugins.
- `samuel install react` resolves to a `repo` + `latest` tag, fetches.
- Users add custom registries in `samuel.toml` (`[[registries]]`).

## The starter-pack meta-plugin

`samuel-starter` is a single plugin that depends on the 12 starter-pack plugins:

```toml
# samuel-plugin.toml in samuel-starter
name = "samuel-starter"
version = "1.0.0"
kind = "meta"                       # new plugin kind

[samuel]
framework = "^2.0.0"

[requires]
create-rfd = "^1.0.0"
create-prd = "^1.0.0"
generate-tasks = "^1.0.0"
code-review = "^1.0.0"
commit-message = "^1.0.0"
document-work = "^1.0.0"
refactoring = "^1.0.0"
security-audit = "^1.0.0"
testing-strategy = "^1.0.0"
troubleshooting = "^1.0.0"
cleanup-project = "^1.0.0"
dependency-update = "^1.0.0"
```

`samuel init` installs `samuel-starter` by default. `samuel init --minimal` skips it.

## Migration steps

Concrete sequence:

1. **Create the registry repo** (`github.com/<org>/samuel-registry`). Empty `index.toml` with `schema_version = 1`.
2. **Bulk-create plugin repos** via a one-time migration script:
   - For each `.claude/skills/<name>/`:
     - Create `github.com/<org>/samuel-<name>`.
     - Copy SKILL.md + scripts/ + references/ + assets/.
     - Generate `samuel-plugin.toml` from the SKILL.md frontmatter (use `metadata.category`, `metadata.language`, `metadata.extensions`).
     - Tag `v1.0.0`.
     - Append entry to `samuel-registry/index.toml`.
3. **Set up GitHub Actions** in each plugin repo for build + cosign + GitHub release.
4. **Create `samuel-starter` meta-plugin** with the 12 starter dependencies pinned.
5. **Test `samuel init`** in a clean directory: should fetch starter-pack, install all 12 plugins, write `samuel.toml` + initial AGENTS.md.

## Built-in content extraction (4 skills)

For the 4 built-in skills, extract content into Go template strings shipped in the v2 binary:

- `auto` → `samuel/internal/methodology/ralph/templates/{prompt,discovery-prompt}.md.tmpl` (see [[entities/auto-prompts]])
- `create-skill` → `samuel/internal/commands/skill/templates/SKILL.md.tmpl`
- `sync-claude-md` / `generate-agents-md` → `samuel/internal/sync/templates/AGENTS.md.tmpl`

The autogen marker stays (`<!-- Auto-generated by Samuel`). Per-project overrides via `.samuel/templates/<area>/<file>.tmpl`.

## Community plugins (7 Anthropic)

Don't bundle. Don't auto-include in starter pack. Just register in the index:

```toml
[plugin.mcp-builder]
repo = "github.com/anthropics/skills"
subpath = "mcp-builder"             # NEW: subpath within repo
latest = "main"                      # community, no tagged release yet
description = "MCP server development guide"
categories = ["workflow"]
upstream = true                      # NEW: marker for "upstream-sourced, hands-off"
```

The plugin fetcher handles `subpath` for community sources that publish multiple skills in one repo. The `upstream` flag tells the registry curators to defer to upstream for content changes.

## Effort estimate

- Plugin migration script: 1 day (Python or Go, reads SKILL.md frontmatter, generates manifest, posts repo).
- Registry index curation: 0.5 day.
- Starter-pack meta-plugin: 0.5 day.
- Built-in template extraction: 0.5 day per area (auto, skill, sync) = 1.5 days.
- End-to-end test: 1 day (`samuel init` in clean dir, verify all 12 starter plugins land).

Total: ~4-5 days of work, mostly mechanical. The bulk of the value (78 high-quality SKILL.md files) is preserved.

## Resolved decisions #v2-decision

- **Plugin repo org**: start at `github.com/ar4mirez/samuel-*` (user's existing org). Migration to a dedicated `github.com/samuel-plugins/*` org is a follow-up if/when the community grows.
- **Starter-pack opt-out granularity**: `samuel init --minimal` skips all 12. `samuel init --without create-rfd,security-audit` skips specific ones (comma-separated list). Implementation: `samuel-starter` meta-plugin reads `--without` from init flags, drops matching requires before resolving.

## Open

- **Versioning at migration time.** All plugins start at `v1.0.0`, or do we mirror the language/framework versions they target? Recommend `v1.0.0` for everything — clean baseline.
- **Community-skill bundling for offline.** If a user is offline, can they still install community plugins? Currently no (requires GitHub fetch). Recommend: ship a small set of community plugins in a `samuel-offline-bundle` meta-plugin for offline use.

## Related

- [[sources/2026-05-12-v1-skill-content-survey]]
- [[concepts/extensibility-design]]
- [[concepts/plugin-format]]
- [[concepts/versioning-compatibility]]
- [[entities/skill-md]]
