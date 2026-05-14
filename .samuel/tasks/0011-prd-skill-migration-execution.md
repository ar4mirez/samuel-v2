---
prd: "0011"
milestone: "v1 skill migration (execution)"
title: Samuel v2.3 — migrate 78 v1 skills to v2 plugin repos
authors:
  - name: ar4mirez
state: Draft
labels: [v2, v2.3, migration, plugins, registry, ecosystem]
created: 2026-05-13
updated: 2026-05-13
target_release: v2.3.x (rolling)
estimated_effort: 2-3 weeks
depends_on: 0009-prd-wasm-plugin-tier.md, 0010-prd-oci-plugin-tier.md
---

# PRD 0011: v1 Skill Migration (execution)

## Wiki references

- [[synthesis/v2-skill-migration-plan]] — the four-bucket plan (built-in / starter / pure / drop) and packaging spec
- [[sources/2026-05-12-v1-skill-content-survey]] — the 78-skill catalog with per-skill triage
- [[concepts/agent-skills-standard]] — the agentskills.io standard the migrated plugins must still conform to
- [[entities/component-gstack-gbrain]] — gstack/gbrain skills are in the drop bucket per [[CLAUDE]]'s "gstack and gbrain dropped from v2"
- RFD 0007 (committed) — Plugin migration from v1 skills

## Summary

Execute the migration plan from RFD 0007. Convert the 78 v1 skills catalogued in v1's `internal/skills/content/` into v2 plugin repos: 4 stay built-in (already done via Go template strings), 12 ship as a `samuel-starter` meta-plugin auto-installed by `samuel init`, 58 become individually-installable plugin repos in the registry, and 2 get dropped (gstack / gbrain). Output: a populated registry, a plugin ecosystem unblocked, and the v1 catalog formally retired.

## Problem statement

v2.0 shipped with a working plugin loader but a near-empty registry. Plugin authors writing for v2 today have no critical mass of examples; users coming from v1 have skill names they remember (`go-guide`, `react`, `create-rfd`) that don't resolve in v2.

The skill-migration plan in [[synthesis/v2-skill-migration-plan]] is a one-time scripted conversion. The plan exists in detail; this PRD is the execution. After this lands, the v2 registry covers every skill v1 users relied on, the starter-pack flow makes `samuel init` give new users a useful baseline immediately, and the v1 binary's skill catalog can be formally deprecated.

This unblocks PRDs 0009 (WASM) and 0010 (OCI) in a different sense: those PRDs ship one reference plugin each, but the broader ecosystem needs critical mass to be credible. Migrating the 58 skills creates that.

## Goals

- **Bucket 1 (built-in, 4 skills)**: verify already-shipped Go template strings in v2 binary cover the four (`overview`, `agents-md`, `methodology`, `guardrails`). No additional work; this PRD just verifies completion.
- **Bucket 2 (starter-pack meta-plugin, 12 skills)**: ship `samuel-starter` as a meta-plugin: one Git repo with a manifest that depends on 12 sub-plugins. Auto-installed by `samuel init` unless `--no-starter` is passed.
- **Bucket 3 (pure plugins, 58 skills)**: one Git repo per plugin, named `samuel-<skill-name>`, signed under the official identity. All in `github.com/samuelpkg/` namespace.
- **Bucket 4 (drop, 2 skills)**: formal removal of `gstack` and `gbrain` skills from v1; explicit "dropped" note in `docs/getting-started/migration-v1.md`.
- **Migration script** at `scripts/migrate-skills/` — repeatable, idempotent, single-command. Anyone with a v1 checkout + GitHub PAT can regenerate the entire set.
- **Registry index regenerated** with all migrated plugins; live registry serves the new index.
- **CHANGELOG** for each migrated plugin: minimum viable history entry preserving the v1 origin commit.
- **End-to-end test**: `samuel install go-guide` from the live registry installs the migrated plugin and `samuel doctor` reports green.
- **Documentation**: `docs/getting-started/migration-v1.md` updated with the per-skill mapping table (v1 name → v2 plugin name).
- **Retirement announcement**: v1 binary marked as `archived` on GitHub; `v1-final` tag preserved; README points at v2 + the migrated plugins.

## Non-goals

- No re-design of the skill content. The 78 skills are ported as-is; behavioral changes are PR-by-PR after migration.
- No new plugin authoring. This is mechanical conversion; new plugins are out of scope.
- No re-licensing. Whatever license v1 ships under, the migrated plugins inherit.
- No batch-renaming of skill names (e.g. "react" → "react-19"). Stable names preserved for migration continuity.
- No translator-plugin migration. Translators stay built-in or skill-level per PRD 0006; this PRD is the *non-translator* skill catalog.
- No private plugin support. Everything migrated lands in public repos.

## Requirements

### Functional

1. **Migration script** at `scripts/migrate-skills/`:
   - `cmd/migrate-skill/main.go` — reads a v1 skill directory (path arg), emits a v2 plugin repo tarball.
   - For each v1 skill it produces:
     - `samuel-plugin.toml` with the manifest wrapper from RFD 0007 (kind, version, samuel framework constraint, provides.skills, capabilities, metadata).
     - `SKILL.md` — copied verbatim from v1 (frontmatter unchanged; the agentskills.io standard requires it).
     - `scripts/`, `references/`, `assets/` — copied if present.
     - `README.md` — auto-generated with: skill name, summary (from SKILL.md frontmatter), origin (v1 commit SHA), license, "migrated from v1 via PRD 0011".
     - `LICENSE` — copied from v1 root.
     - `CHANGELOG.md` — minimal: `## [1.0.0] — initial release (migrated from v1 commit <sha>)`.
     - `.gitignore` — standard Go-language plugin ignore (no Go files, but futureproof for plugins that grow).
   - Idempotent: re-running over the same v1 skill produces identical output (modulo metadata timestamps which are stripped).
   - Determinism: file ordering, manifest field ordering, all stable.

2. **Repo provisioning script** at `scripts/migrate-skills/cmd/provision-repos/main.go`:
   - For each migrated plugin tarball, creates the GitHub repo if absent, force-pushes the initial commit, sets repo metadata (description, topics, homepage), creates a `v1.0.0` release tag.
   - Requires a GitHub PAT with `repo` scope; surfaced via `GITHUB_TOKEN` env.
   - Dry-run mode (`--dry-run`) lists what would be created without making changes.
   - Rate-limit aware: backoffs on 429.

3. **Signing** — each plugin's first release is signed:
   - GitHub Actions workflow template `release-with-sign.yml` (committed to each migrated plugin repo via the migration script).
   - Workflow signs the release tarball with cosign keyless OIDC under the official identity pattern `https://github.com/samuelpkg/samuel-*/...`.
   - Manual run on `v1.0.0` tag for the migration batch; subsequent releases auto-trigger.

4. **Starter-pack meta-plugin** (`github.com/samuelpkg/samuel-starter`):
   - `samuel-plugin.toml` with `kind = "meta"` (new kind) and a `[dependencies]` block listing the 12 starter plugins.
   - `samuel install <meta>` resolves and installs all dependencies; uninstall removes them as a group.
   - Loader update: `internal/plugin/service/` learns the `meta` kind, treats it as a transactional batch install.
   - `samuel init` default behavior: install `samuel-starter` unless `--no-starter` flag.
   - `samuel init --starter=other-pack` allows custom starter packs (future ecosystem feature; v2.3 ships only the official one).

5. **Registry index regeneration**:
   - `scripts/registry-generator/` (existing tool from v2.0) re-runs against the populated namespace.
   - New `index.toml` lists all 71 published plugins (58 pure + 12 starter sub-plugins + 1 meta-plugin = 71 entries; 4 built-ins not in registry).
   - Tags per plugin auto-populated from `samuel-plugin.toml` `[metadata]`.

6. **Documentation updates**:
   - `docs/getting-started/migration-v1.md` updated with a v1 → v2 mapping table:
     ```
     | v1 skill name | v2 plugin | Notes |
     | --- | --- | --- |
     | go-guide | samuel-go-guide | bucket: pure |
     | react | samuel-react | bucket: pure |
     | overview | (built-in) | embedded in v2 binary |
     | gstack | (dropped) | see RFD 0008 |
     | ... | ... | ... |
     ```
   - `docs/plugin-authors/` — link to one migrated plugin as a real-world example.
   - `docs/concepts/registry.md` updated with the populated state.
   - `docs/blog/v2.3-migration-complete.md` (new) — public announcement.

7. **Live e2e test additions** (extends `e2e/live/` from PRD 0007):
   - `TestMigration_InstallsTopFive` — install the 5 most-common v1 skills (`go-guide`, `react`, `python`, `typescript`, `create-rfd`); assert all install + report healthy in `samuel doctor`.
   - `TestMigration_StarterPackInstalls` — `samuel init` against a fresh tempdir installs `samuel-starter` and resolves all 12 deps.
   - `TestMigration_DroppedSkillResolvesToNothing` — `samuel install gstack` errors with a structured error pointing at the migration doc.

8. **v1 archive announcement**:
   - `github.com/samuelpkg/samuel-v1` repo (or whatever holds v1 today): archived flag set on GitHub.
   - README replaced with a stub pointing at v2 + the migrated plugin namespace.
   - `v1-final` tag preserved (already created in PRD 0006); no force-push beyond what PRD 0006 already did.
   - Migration doc explicitly invites issues to be filed against the v2 plugin repos directly.

9. **Provenance**:
   - Each migrated plugin's README carries a `Migrated-From: <v1 commit SHA>` line and a `Migrated-At: <date>` line.
   - The migration script writes these from the v1 commit it read.
   - Useful for future audit; not user-facing critical path.

### Non-functional

- Migration script runs in <10 minutes for the full 71 plugins (excluding GitHub API rate limits).
- Each migrated plugin's repo is small (<100 KB content; no large assets).
- Migration is reversible up until force-push: the script writes to a staging directory; provision-repos consumes it as a separate step.
- All migrated plugins' release workflows verified individually (one workflow run per repo).
- Skill content unchanged: a diff of `SKILL.md` between v1 and the migrated plugin's tree is byte-identical.
- No plugin renamings without a deprecation alias. v1's `react` becomes `samuel-react` (plugin slug `react`); `samuel install react` resolves correctly.

## Acceptance criteria

- [ ] `scripts/migrate-skills/cmd/migrate-skill/` builds, runs, produces deterministic output across two runs.
- [ ] All 58 pure-bucket plugins exist as repos under `github.com/samuelpkg/samuel-*`, signed, with `v1.0.0` release tags.
- [ ] `samuel-starter` meta-plugin exists, declares the 12 dependencies, installs them in one transaction.
- [ ] Registry `index.toml` lists 71 plugins; `samuel search go` returns the migrated `go-guide`.
- [ ] `samuel install go-guide` succeeds against live registry; `samuel doctor` reports green.
- [ ] `samuel init` in a fresh tempdir installs `samuel-starter` + 12 deps by default; `--no-starter` skips.
- [ ] `samuel install gstack` returns a structured error pointing at the migration doc.
- [ ] `docs/getting-started/migration-v1.md` carries the complete v1 → v2 mapping table.
- [ ] `docs/blog/v2.3-migration-complete.md` published.
- [ ] `github.com/samuelpkg/samuel-v1` (or equivalent) archived; README rewritten.
- [ ] Live e2e tests `TestMigration_*` all green.
- [ ] CHANGELOG v2.3.x entry summarizes the migration (count of plugins, link to mapping table).
- [ ] At least 30 migrated plugins have been manually spot-checked (random sample) and the SKILL.md byte-identity verified.

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Migration script bug corrupts one plugin's content silently | High | Byte-identity check on `SKILL.md`; spot-check 30 random plugins; CI gate that compares pre- and post-migration `SKILL.md` SHAs |
| GitHub API rate limits exhausted mid-migration | High | Backoff + retry; run in batches of 10; document the multi-day timeline if needed |
| Cosign keyless OIDC failures on the first batch of signings | Medium | Re-run the signing workflow per plugin; document the recovery path |
| Starter-pack default-install surprises users in CI | Medium | `samuel init --no-starter` flag prominent in CI docs; structured-error message recommends it for CI; default-installs documented loudly in CHANGELOG |
| A skill in the "pure" bucket has a hidden dependency on a v1-only API | Medium | Pre-audit each bucket-3 skill's SKILL.md for v1 framework references; flag any that need rewrite before migration |
| Plugin name collisions in `github.com/samuelpkg/` (a skill named the same as a samuel-* org repo) | Low | Pre-flight check in the migration script; if collision detected, fail loudly with rename recommendation |
| User-installed v1 skills with local modifications get lost in the migration | High | Migration doc warns users; recommend they fork the migrated plugin repo and re-apply their changes; no auto-merge attempt |
| `gstack` / `gbrain` users complain | Medium | Migration doc explains the drop with rationale (per RFD 0008); link to the upstream gstack project for users who want it as a standalone tool |
| Meta-plugin transactional install partially fails (10 of 12 sub-plugins succeed) | Medium | Rollback on partial failure; structured error lists what failed; document the recovery path (`samuel install samuel-starter --retry`) |

## Open questions

- **Plugin org**: `github.com/samuelpkg/` (recommended) or a new `github.com/samuel-plugins/` org? Recommend `samuelpkg/` to keep things consolidated; revisit only if the namespace gets crowded.
- **Plugin discovery beyond the official registry**: support third-party registries declared in `samuel.toml`? Yes, the registry surface already supports this; document in `docs/concepts/registry.md` post-migration.
- **CI bot user for signing**: `samuel-bot` GitHub identity, or per-repo workflows under `samuelpkg/`? Per-repo workflows (simpler, no shared identity).
- **What about v1 skills that have already been forked into the wild?** Migration doc invites forks to file a "migrated-fork" PR against the official migrated repo if they want their changes upstreamed; otherwise they coexist.
- **Starter-pack composition**: which 12? Per [[synthesis/v2-skill-migration-plan]], but the final list will be confirmed during migration. Suggested defaults: `go-guide`, `python`, `typescript`, `react`, `nextjs`, `claude-code-tips`, `pr-review`, `test-writer`, `commit-message`, `4d-methodology`, `create-rfd`, `progress-log`.

## Task hints

1. Confirm bucket assignments against [[synthesis/v2-skill-migration-plan]] and the v1 skill catalog; document the final 4/12/58/2 split
2. Write `cmd/migrate-skill/main.go` — single-skill conversion to a staging directory
3. Add byte-identity check for `SKILL.md` in unit tests
4. Run conversion against a sample of 5 skills; manually verify output structure
5. Write `cmd/provision-repos/main.go` — staging directory → GitHub repos
6. Add `--dry-run` mode; run full dry-run to validate the 58-plugin list
7. Write `release-with-sign.yml` workflow template (committed to each migrated repo)
8. Provision the 58 pure-bucket repos in 6 batches of ~10 (rate-limit safe)
9. Provision the 12 starter-pack sub-plugin repos
10. Provision `samuel-starter` meta-plugin repo with `[dependencies]` block
11. Implement `meta` kind in `internal/plugin/service/` with transactional install
12. Implement `--no-starter` flag on `samuel init`
13. Implement dropped-skill error in `internal/plugin/service/install.go` (well-known dropped names with structured error)
14. Re-run `scripts/registry-generator/` against the populated namespace
15. Verify registry `index.toml` lists 71 entries
16. Write `e2e/live/migration_live_test.go`
17. Update `docs/getting-started/migration-v1.md` with the mapping table
18. Generate the table programmatically from migration-script metadata so it can't drift
19. Update `docs/concepts/registry.md`
20. Draft `docs/blog/v2.3-migration-complete.md`
21. Archive `github.com/samuelpkg/samuel-v1` (or equivalent v1 repo); rewrite its README
22. CHANGELOG v2.3.x entry summarizing the migration
23. Spot-check 30 random migrated plugins; record findings
24. Publish announcement (cross-link blog post)
