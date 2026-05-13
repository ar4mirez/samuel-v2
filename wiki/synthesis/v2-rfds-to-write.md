---
title: v2 RFDs to write
type: synthesis
created: 2026-05-12
updated: 2026-05-12
sources: [2026-05-12-v1-rfds]
tags: [v2, v2-decision, rfd, roadmap]
---

# v2 RFDs to write

Every major v2 design decision filed in the wiki should be converted to an RFD in `samuel/docs/rfd/`. The wiki is exploratory; the RFDs are the **public, queryable, attributable** historical record.

This page is the migration list from wiki concepts → numbered RFDs.

## The inaugural set

Eight RFDs covering the v2 architecture. Each is a clean port of an existing wiki page into the RFD body structure ([[concepts/rfd-process]]).

| # | Title | State at v2.0 ship | Source wiki page |
|---|---|---|---|
| 0001 | Three-tier plugin architecture (skill / WASM / OCI) | Committed | [[concepts/plugin-format]] |
| 0002 | AGENTS.md primary, tool-specific files via translator plugins | Committed | [[concepts/agents-md-primary]] |
| 0003 | SemVer + capability model + Sigstore signing | Committed | [[concepts/versioning-compatibility]] |
| 0004 | Methodology hooks (default built-in + plugin enhancement) | Committed | [[concepts/methodology-default-plus-plugin]] |
| 0005 | Component-lifecycle interface as v2 plugin loader | Committed | [[synthesis/orchestrator-as-plugin-loader]] |
| 0006 | `samuel run [methodology]` rename + Ralph Wiggum as default | Committed | [[synthesis/auto-mode-v2-design]] |
| 0007 | Plugin migration from v1 skills (one repo per plugin, registry index) | Committed | [[synthesis/v2-skill-migration-plan]] |
| 0008 | Drop gstack and gbrain (v2 clean break) | Committed | [[entities/component-gstack-gbrain]] |

## Convention

- File path: `samuel/docs/rfd/0001.md` (4-digit zero-padded).
- Frontmatter: per [[concepts/rfd-process]] (`rfd`, `title`, `state`, `authors`, `labels`, `created`, `updated`, `discussion`, `related_prd`).
- Body structure: Summary → Problem → Background → Options → Decision → Implementation → Outcome.

The wiki source page provides most of the body content. The RFD adds:

- **Options Considered section** — what was the alternative? Include the rejected paths even when the choice was obvious to us (future-you may not find it obvious).
- **Effort estimate** per option.
- **Outcome** section, filled post-implementation.

## Why convert wiki → RFD

The wiki is a thinking artifact — exploratory, dense with cross-references, optimized for *Claude*'s pattern-matching across pages. The RFDs are user-facing documentation — linear, scannable, optimized for a human reading "why did Samuel choose X over Y?" two years from now.

Same content, different audiences. Both should exist.

## Ordering for v2.0 ship

Suggested write order (RFDs depend on each other; write the foundations first):

1. RFD 0005 — Component-lifecycle interface (the plugin loader basis).
2. RFD 0001 — Three-tier plugin architecture (uses the loader).
3. RFD 0003 — SemVer + capability model (uses the plugin shape).
4. RFD 0008 — Drop gstack/gbrain (clears the v1 baggage).
5. RFD 0007 — Plugin migration (uses 0001 + 0003 + 0008).
6. RFD 0002 — AGENTS.md primary (cross-tool stance).
7. RFD 0004 — Methodology hooks (uses 0001).
8. RFD 0006 — `samuel run [methodology]` (uses 0004).

Each RFD references its dependencies in the `labels` and inline links.

## Future RFDs (post-v2.0)

Things deferred but worth eventually documenting:

- **RFD 0009** — Plugin signing via Sigstore enforcement (currently opt-in).
- **RFD 0010** — Multi-version plugin coexistence (versioned plugin namespaces).
- **RFD 0011** — Cross-agent prompt translation layer (codifying [[concepts/multi-agent-support]]).
- **RFD 0012** — Hot-reload of plugins without `samuel run` restart.

Capture as Prediscussion in `.samuel/rfd/` when work begins.

## What happens to v1's RFDs?

v1's RFDs 0001-0004 ([[sources/2026-05-12-v1-rfds]]) stay in v1's repo as historical record. They are **not** ported to v2 — v2 is a clean break, and v2's RFD 0001 is a different decision (plugin architecture, not progressive disclosure).

The wiki entry on v1's RFD 0001 (progressive disclosure) carries forward as **a lesson**: bloat happens, set hard limits with CI checks. See [[synthesis/v2-template-and-docs]].

## Open

- **Authorship in v2 RFDs.** Single author (ar4mirez) for the inaugural eight, or multiple? Probably single for v2.0 — clear ownership of the rebuild.
- **Discussion mechanism.** GitHub Issues, Discussions, PRs? Recommend Discussions for RFDs in `Discussion` state.
- **Cross-link to wiki.** Should each v2 RFD include a "Source notes: see Samuel wiki [page]" footer? Pro: traceability. Con: leaks internal exploration into public docs. Defer.
