---
prd: "0006"
milestone: "Polish + Launch"
title: Samuel v2 Polish + Launch — Charm UI, docs, RFDs, v2.0.0 ship
authors:
  - name: ar4mirez
state: Draft
labels: [v2, polish, launch, docs, rfds, charm, release]
created: 2026-05-12
updated: 2026-05-12
target_release: v2.0.0
estimated_effort: 2 weeks
depends_on: 0005-prd-skill-migration.md
---

# PRD 0006: Samuel v2 Polish + Launch

## Wiki references

- [[entities/ui-package]] — Charm library swap recommendation
- [[synthesis/v2-template-and-docs]] — docs restructure + AGENTS.md template ≤150 lines
- [[synthesis/v2-rfds-to-write]] — eight inaugural RFDs to commit
- [[sources/2026-05-12-v1-build-release]] — release infrastructure (with v2 additions: cosign, SBOM, OCI image)
- [[concepts/agnostic-by-design]] — CI invariant check
- [[concepts/structured-errors]] — error UX polish

## Summary

Ship v2.0.0. Polish the CLI UX with the Charm library swap (lipgloss + huh + bubbles). Lock in the AGENTS.md template at ≤150 lines with the rendered-output CI check. Write the eight inaugural RFDs. Restructure the docs site. Add the agnostic-by-design CI invariant. Tag v2.0.0-rc → v2.0.0 over a beta period; coordinate the v1 deprecation messaging.

## Problem statement

After Milestone 5, v2 is functionally complete: framework, plugin loader, methodology, 78+ migrated plugins, two translators. What remains is polish, documentation, the public RFD record, and the release coordination.

Quality of these matters because v2 is a clean break — the launch must look intentional, the docs must answer "why" questions before users ask, and the RFDs must explain every major design choice. The launch is the moment the v1 → v2 migration story lands publicly.

## Goals

- **Charm UI swap** complete: `lipgloss` (color/layout), `huh` (prompts, including native multi-select), `bubbles` (spinner/progress).
- **AGENTS.md template ≤150 lines** with CI check on rendered output (not just source).
- **Agnostic-by-design CI check** added to release workflow.
- **Eight inaugural RFDs** committed at `samuel_v2/docs/rfd/0001.md` through `0008.md`.
- **Docs site** restructured per [[synthesis/v2-template-and-docs]]: drop language/framework/workflow dirs, add `docs/concepts/` and `docs/plugin-authors/`, generate `docs/plugins/` from registry index.
- **Migration notice from v1** at `samuel_v2/docs/getting-started/migration-v1.md` — not an upgrade guide, just "here's what's different and how to start fresh."
- **Release**: v2.0.0-rc.2 → v2.0.0-rc.3 → v2.0.0 over a 2-3 week beta.
- **v1 deprecation messaging**: the `github.com/samuelpkg/samuel` repo's README replaced with the v2 README; old v1 code preserved via `v1-final` tag.

## Non-goals

- New features. v2.0 is locked at end of Milestone 5.
- Plugin authoring CLI. Deferred to v2.1+.
- Translator plugins beyond claude + codex. Deferred.
- Domain registration (`samuel.dev`). Deferred per earlier decision; stay on GitHub Pages.

## Requirements

### Functional

1. **Charm library migration** at `internal/ui/`:
   - Replace `fatih/color` with `charmbracelet/lipgloss` for color/style tokens.
   - Replace `manifoldco/promptui` with `charmbracelet/huh` for prompts:
     - Native multi-select (drops v1's awkward custom impl).
     - Validation built in.
     - Form-style chaining for multi-prompt flows.
   - Replace `schollz/progressbar/v3` with `charmbracelet/bubbles/{spinner,progress}`.
   - Keep the same six-category vocabulary (success/error/warn/info/bold/dim).
   - Keep the same JSON envelope (schemaVersion 4).
   - Caller API surface unchanged (`ui.Success(...)`, `ui.Select(...)`, etc.).

2. **AGENTS.md template polish**:
   - Final review of `template/AGENTS.md.tmpl`.
   - Sections that survived (per [[synthesis/v2-template-and-docs]]):
     - 4D Methodology (~30 lines)
     - Boundaries (~15 lines)
     - Quick Reference (~20 lines)
     - `<!-- SAMUEL_PLUGINS_START/END -->` block (auto-populated)
     - `<!-- SAMUEL_GUARDRAILS_START/END -->` block (from `samuel.toml`)
     - Project Context (fillable, ~10 lines)
   - Rendered output ≤ 150 lines (CI enforced via `agents-md-check.yml`).

3. **Agnostic-by-design CI check** at `.github/workflows/agnostic-check.yml`:
   - Grep for `"CLAUDE\.md"` literals and `".claude/"` paths in `internal/`.
   - Fail with structured error pointing to [[concepts/agnostic-by-design]].
   - Plugins exempt (only checks framework code).

4. **Eight inaugural RFDs** at `docs/rfd/`:
   - RFD 0001 — Three-tier plugin architecture (port from [[concepts/plugin-format]]).
   - RFD 0002 — AGENTS.md primary, tool-specific via translator plugins (port from [[concepts/agents-md-primary]]).
   - RFD 0003 — SemVer + capability model + Sigstore signing (port from [[concepts/versioning-compatibility]]).
   - RFD 0004 — Methodology hooks (default built-in + plugin enhancement) (port from [[concepts/methodology-default-plus-plugin]]).
   - RFD 0005 — Component-lifecycle interface as plugin loader (port from [[synthesis/orchestrator-as-plugin-loader]]).
   - RFD 0006 — `samuel run [methodology]` rename + Ralph Wiggum as default (port from [[synthesis/auto-mode-v2-design]]).
   - RFD 0007 — Plugin migration from v1 skills (port from [[synthesis/v2-skill-migration-plan]]).
   - RFD 0008 — Drop gstack and gbrain (port from [[entities/component-gstack-gbrain]]).
   - Each RFD follows v1's structure: Summary, Problem, Background, Options Considered (with Pros/Cons/Effort per option, including rejected alternatives), Decision, Implementation, Outcome.
   - `rfd-index.toml` populated.

5. **Docs site restructure** at `samuel_v2/docs/`:
   - Keep `core/` (overview, agents-md, methodology, guardrails, plugins).
   - Keep `getting-started/` (installation, quick-start, first-task, **migration-v1.md** new).
   - Keep `reference/` (cli, faq, cross-tool, changelog).
   - Drop `languages/`, `frameworks/`, `workflows/` (were SKILL.md duplicates).
   - Add `concepts/` — port wiki concepts to user-facing form.
   - Add `plugin-authors/` — guide to writing plugins (manifest, hooks, capabilities, TinyGo WASM toolchain).
   - Add `plugins/` — auto-generated index from `samuel-registry/index.toml`, one page per plugin with README content pulled.
   - Update `mkdocs.yml` nav structure.

6. **Migration notice** at `samuel_v2/docs/getting-started/migration-v1.md`:
   - Not an upgrade guide. "Here's what's different."
   - For v1 users: how to install v2 alongside v1 (different binary names? coexistence path?), how to migrate a project, what survives, what doesn't.
   - For v1 plugin authors (theoretical): how to publish v2 plugins.
   - Calls out the clean-break stance explicitly.

7. **v1 deprecation messaging** in `github.com/samuelpkg/samuel`:
   - On v2.0 release: force-push `main` to v2's code.
   - Tag `v1-final` at the last v1 commit before the push.
   - v1's README replaced with v2's README + deprecation notice ("v1 is preserved at the `v1-final` tag").
   - Homebrew tap formula updated to v2.
   - `install.sh` updated.

8. **Changelog** at `samuel_v2/CHANGELOG.md`:
   - Fresh file. v2.0.0 as the first entry.
   - Discipline matching v1's v3.0.0 entry (rationale per item, internal section, test coverage stats, deprecation timeline).
   - Link from v2's README.

9. **Release sequence**:
   - **v2.0.0-rc.2**: feature freeze. Charm swap complete. RFDs drafted. Docs site building. Internal test.
   - **v2.0.0-rc.3** (after 1 week): bug fixes from rc.2. Migration notice finalized. v1 deprecation messaging drafted.
   - **v2.0.0** (after another week): final. v1 repo force-pushed. Homebrew tap updated. Announce.

10. **Announcement**:
    - Blog post draft at `docs/blog/samuel-v2-launch.md` (or external).
    - Key points: rebuild rationale, plugin architecture, agnostic-by-design, Ralph methodology survives, TOON runtime, what's new for users (`samuel install <plugin>`).
    - Cite the RFDs.
    - Acknowledge gstack/gbrain drop (some v1 users may care).

### Non-functional

- `samuel --help` output is clean (no orphan flags, consistent verb descriptions).
- All commands implement `--json` (regression test).
- All structured errors include `Fix:` and `DocsURL:` fields.
- mkdocs site builds cleanly with no broken links (`mkdocs build --strict`).
- Release artifacts are signed, SBOM included, OCI image published.

## Acceptance criteria

- [ ] `samuel --help` uses Charm color/style; verbs aligned.
- [ ] `samuel install` prompts use `huh` with native multi-select.
- [ ] `samuel run start` shows `bubbles/spinner` between iterations.
- [ ] Rendered AGENTS.md (output of `samuel init` in a sample project) ≤ 150 lines.
- [ ] `agents-md-check.yml` CI gate passes; fails the PR if exceeded.
- [ ] `agnostic-check.yml` CI gate passes; would fail if `"CLAUDE.md"` literal appears in `internal/`.
- [ ] All 8 RFDs committed and rendered in mkdocs.
- [ ] `rfd-index.toml` lists all 8.
- [ ] `mkdocs build --strict` succeeds.
- [ ] `docs/plugins/` auto-generates from `samuel-registry/index.toml`.
- [ ] `docs/getting-started/migration-v1.md` published.
- [ ] CHANGELOG.md v2.0.0 entry matches v1 v3.0.0 quality.
- [ ] v2.0.0-rc.2 tag → goreleaser publishes signed artifacts.
- [ ] After 1-week soak: v2.0.0-rc.3 tag, fixes incorporated.
- [ ] After another week: v2.0.0 tag. Public announce.
- [ ] `github.com/samuelpkg/samuel` `main` force-pushed to v2. `v1-final` tag preserved.
- [ ] `brew install samuel` installs v2.0.0.
- [ ] `curl -sSL <install.sh> | sh` installs v2.0.0.
- [ ] `samuel doctor` passes on a fresh `samuel init`.

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Charm library API has changed since 2024 design notes | Medium | Pin versions during Milestone 1. Update during this milestone if needed. Charm's API is stable. |
| AGENTS.md ≤150 lines breaks under full guardrails config | Medium | CI tests on a "maximum config" fixture. If breaks, prune content (move to docs) before launch. |
| RFD writing takes longer than estimated (1 week budget) | Medium | RFDs are ports of wiki concept pages — ~2 hours each. Write in parallel. Drop "Outcome" section for v2.0 RFDs (post-implementation reflection comes later). |
| v1 users complain about clean-break (no upgrade path) | High | Migration notice explains rationale. v1-final tag preserves access. v1 still installable from old releases. |
| Force-push to `github.com/samuelpkg/samuel` breaks watchers / forks | High | Announce 1 week prior. Use `v1-final` tag. Document in migration notice. |
| Docs `plugins/` page generation needs runtime data (registry index) | Medium | Generate at docs build time via a script that fetches `samuel-registry/index.toml`. Cache in CI. |
| Translator plugins (claude + codex) not stable enough at launch | Medium | If unstable, ship `claude-translator` only; mark `codex-translator` as `0.x` experimental. Both prove the pattern; one is sufficient for launch. |
| Mkdocs build fails on missing plugin docs | Low | Use `mkdocs build` without `--strict` for the plugins dir; warnings only. Address post-launch. |

## Open questions

- **Soak time between rc.2 and v2.0.0**: 2 weeks (current plan) vs 4 weeks? Recommend 2 weeks; small user base, fast feedback cycles.
- **Blog post**: hosted where? GitHub Discussions, Anthropic blog (if relevant), personal site? User's call.
- **v1 binary coexistence**: should v2's `samuel` install path conflict with v1's? They're the same binary name. Same path. v2 overwrites. Documented.
- **`samuel auto` alias**: confirmed permanent (per [[entities/command-tree-v1]]). Listed in v2's `samuel --help`? Probably hidden but functional.

## Task hints

1. Audit current Charm library versions; pin in `go.mod`
2. Swap `fatih/color` → `lipgloss` in `internal/ui/output.go`
3. Swap `promptui` → `huh` in `internal/ui/prompts.go`
4. Swap `schollz/progressbar` → `bubbles/spinner` + `bubbles/progress`
5. Regression test: every UI helper produces equivalent output
6. Render AGENTS.md from `samuel init` against max-config fixture, measure lines
7. Trim AGENTS.md template if needed
8. Write `agents-md-check.yml` against rendered output
9. Write `agnostic-check.yml`
10. Draft RFD 0005 first (foundational)
11. Draft RFDs 0001, 0003 in parallel
12. Draft RFDs 0002, 0004, 0006 in parallel
13. Draft RFD 0007 (migration)
14. Draft RFD 0008 (drop gstack/gbrain)
15. Populate `rfd-index.toml`
16. Restructure `docs/` per plan
17. Port wiki concepts to `docs/concepts/`
18. Write `docs/plugin-authors/` guide
19. Build `docs/plugins/` generator script (pulls from registry)
20. Write `docs/getting-started/migration-v1.md`
21. Update `mkdocs.yml` nav
22. Run `mkdocs build --strict` clean
23. Draft v2.0.0 CHANGELOG entry
24. Tag v2.0.0-rc.2 + smoke test
25. After 1 week: incorporate feedback, tag v2.0.0-rc.3
26. After another week: tag v2.0.0
27. Force-push `github.com/samuelpkg/samuel` (after `v1-final` tag)
28. Update Homebrew tap
29. Publish migration notice prominently
30. Write + publish announcement
