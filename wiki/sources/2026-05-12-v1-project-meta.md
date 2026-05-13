---
title: v1 Project Meta (README, CLAUDE.md root, AGENTS.md root, CHANGELOG)
type: source
created: 2026-05-12
updated: 2026-05-12
sources: []
tags: [v1, meta, changelog]
---

# v1 Project Meta

Ingest pass 12. The repo-root meta files that present the project externally.

## Files

- `samuel_v1/README.md` (~660 lines) — public-facing project introduction
- `samuel_v1/CLAUDE.md` (340 lines) — repo-root dogfood (NOT identical to template)
- `samuel_v1/AGENTS.md` (316 lines) — repo-root dogfood (NOT identical to template)
- `samuel_v1/CHANGELOG.md` (519 lines) — 12 releases from 1.0.0 (2025-01-14) through 3.0.0 (2026-04-29)
- `samuel_v1/LICENSE` — MIT

## Key claims

### README.md

Strong hero:

> **`samuel run` and walk away.**
> Ralph Wiggum methodology • Cross-tool (Claude Code, Cursor, Codex, Copilot) • Opinionated guardrails baked in

Positioning: "Samuel is a Go CLI that ships an autonomous AI coding loop plus a library of language, framework, and workflow guides that any AI tool can read. The differentiator is `samuel run` — point it at a structured task list, walk away, and let the loop pick tasks, implement them, run tests, and commit."

Three install paths: Homebrew (`brew tap ar4mirez/tap && brew install samuel`), curl (`install.sh`), Go (`go install`).

Quick Start in 60 seconds. Shows `samuel ls --all`, `samuel add typescript`, `samuel run init` + `start`, `samuel run` (bare = status).

CHANGELOG v3.0.0 entry notes "README hero rewrite. v2's generic 'Build smarter, faster, more scalable software' tagline replaced with the autonomous-loop wedge: 'the autonomous AI coding loop your CLI needs — `samuel run` and walk away.' Direct response to /autoplan CEO + DX phase findings."

**Implication**: v1 used its own autonomous mode + advisor patterns (`/autoplan`, CEO + DX phases) to plan its own v3.0.0 release. Dogfood proof at the planning level, not just code level.

### Root CLAUDE.md / AGENTS.md vs template

| File | Lines | Purpose |
|---|---|---|
| `template/CLAUDE.md` | 474 | Installed into user projects via `samuel init` |
| `template/AGENTS.md` | 474 | Cross-tool mirror of template/CLAUDE.md |
| Repo-root `CLAUDE.md` | 340 | v1's own dogfood for the Samuel repo |
| Repo-root `AGENTS.md` | 316 | v1's own dogfood, cross-tool mirror |

**The root and template files have diverged.** Root is leaner (340 / 316 lines), closer to RFD 0001's original ~280-line target. Template stayed bloated. The lean version exists — it just isn't the one users get.

This is a real opportunity for v2: ship the **leaner shape** as the user template, not the bloated one.

### CHANGELOG.md

Twelve releases over ~15 months. Format: Keep a Changelog standard ([Unreleased], dated [version] headers, Added/Changed/Deprecated/Removed/Internal sections).

Release cadence:
- v1.0.0 → v1.8.0: 12 months (slow ramp through 2025).
- v2.0.0: Feb 2026 (the `.agent/` → `.claude/` migration).
- v3.0.0: April 2026 (the lean reshape: 14 → 11 commands, `auto` → `run` rename).
- [Unreleased]: pending changes after v3.0.0.

The 3.0.0 entry is **exceptional documentation**:

- Thesis stated upfront ("the lean reshape").
- Every added/changed/deprecated item with rationale.
- Migration paths called out.
- Internal section names specific files changed (`internal/commands/legacy.go`, `internal/commands/inference.go`, `internal/commands/json_helpers.go`).
- Test coverage stats ("60+ new tests across the v3 surface").
- Hidden+Deprecated wrapper pattern fully documented.
- "v3.1.0 (~3 months)" — concrete deprecation timeline.

Compare to most projects: "Renamed `auto` to `run`. Various fixes." This is what good changelogs look like.

### The internal version timeline (corrected)

Now I can reconstruct it fully:

| External v1 version | Date | Notable |
|---|---|---|
| 1.0.0 | 2025-01-14 | Initial release |
| 1.1.0 | 2025-01-15 | (early iterations) |
| 1.2.0–1.7.0 | 2025-12 to 2026-01 | (rapid iteration) |
| 1.8.0 | 2026-02-04 | **Agent Skills integration** — 25+ tool compat, `samuel skill` commands |
| 2.0.0 | 2026-02-12 | **`.agent/` → `.claude/` migration**, single-file CLAUDE.md (was 3 files) |
| 3.0.0 | 2026-04-29 | **Lean reshape**: 14 → 11 commands, `auto` → `run`, type inference |
| Unreleased | current | pending changes — this is the v1 we've been ingesting |

So the user's external "v1" is internally **post-3.0.0 with Unreleased changes pending**. The "v2 we're rebuilding" maps roughly to internal "v4.0.0" (a hypothetical major bump).

### Other meta artifacts

- **`LICENSE`**: MIT. Standard.
- **`.gitignore`** (not read): conventional Go project gitignore.
- **`requirements-docs.txt`** (referenced in Makefile): mkdocs-material dependencies for docs build.
- **`.github/RELEASE_CHECKLIST.md`**: manual release checklist (not read but worth noting for v2).
- **`docs/getting-started/migration-v3.md`**: v2 → v3 migration guide (worth porting the **shape** for v2's "what's changed" notes).

## Assessment

- **Credibility**: high.
- **Quality of project meta**: exceptional. README is well-positioned. CHANGELOG is best-in-class. The dogfooding-of-methodology (using `/autoplan` to plan v3.0.0) is a credible "we use what we ship" story.
- **Internal version drift**: external v1 is internally post-3.0.0 with pending Unreleased changes. The user's "v2" maps to a hypothetical v4.0.0 — major version bump, clean break, justified.

## v2 implications

### `#rescue`

- **README hero pattern**. Tagline + bullets of cross-tool support + install paths + 60-second Quick Start. Port the structure; update specifics for v2 (`samuel install <plugin>`, not `samuel add`).
- **CHANGELOG discipline**. Keep a Changelog format. Detailed entries with rationale. Test coverage stats. Specific filenames in Internal sections. v2's CHANGELOG should match this quality.
- **Repo-root dogfood that's leaner than the template**. Use the same shape: framework's own AGENTS.md is the strictest example of the AGENTS.md the framework expects to generate. Self-impose the line limit before asking users to.
- **Hidden+Deprecated wrapper pattern** ([[entities/command-tree-v1]] legacy aliases) for any future rename in v2.x. Not needed at v2.0 launch (clean break) but useful infrastructure.

### `#refactor`

- **Hero tagline for v2**. v1's "the autonomous AI coding loop your CLI needs" stays mostly intact, but the framework reshape changes the supporting bullets. New angle: "framework + skills hub for AI coding assistants" or similar.
- **README install paths**. Same shape, update URLs.
- **CHANGELOG version timeline**. v2.0.0 will be a new line in this CHANGELOG (or a fresh file in `samuel/`).

### `#drop`

- **`.agent/` migration notes** (from v2.0.0 CHANGELOG). Irrelevant to v2.
- **migration-v3.md content**. v1's v2→v3 mapping. New v2 doesn't need an upgrade guide (clean break) — just a "what changed since v1" reference for context.

### `#open`

- **Where does v2's CHANGELOG start?** Fresh file at `samuel/CHANGELOG.md` with v2.0.0 as the first entry, or continue v1's file? Recommend: fresh file. v2 is a clean break, the CHANGELOG should reflect that.
- **`/autoplan` workflow** — v1 used some kind of structured-planning workflow with CEO and DX advisor "phases." Not part of the v1 codebase I ingested. Likely external Claude conversations or PRD-driven planning. Worth asking the user about for v2.

## Related pages

- [[entities/samuel-v1]] — the anchor, now reconciled with full version history
- [[entities/samuel-v2]] — the rebuild target
- [[sources/2026-05-12-v1-template-docs]] — template/CLAUDE.md vs root CLAUDE.md analysis
- [[sources/2026-05-12-v1-rfds]] — RFD 0001 cautionary tale, now confirmed by the root/template divergence
