# Tasks — PRD 0011: v1 Skill Migration (execution)

> Generated from [0011-prd-skill-migration-execution.md](0011-prd-skill-migration-execution.md) on 2026-05-13.
> Depends on PRDs 0009 (WASM) and 0010 (OCI) being complete.
> Target release: v2.3.x (rolling).

## Relevant files

- `samuel_v1/.claude/skills/` — 78 source SKILL.md directories (v1 reference checkout)
- `samuel_v1/internal/skills/content/` — byte-identical mirror; same source
- `wiki/sources/2026-05-12-v1-skill-content-survey.md` — full triage (4/12/58/2 bucket assignments)
- `wiki/synthesis/v2-skill-migration-plan.md` — migration approach (RFD 0007)
- `scripts/migrate-skills/cmd/migrate-skill/main.go` — NEW (single-skill conversion)
- `scripts/migrate-skills/cmd/provision-repos/main.go` — NEW (batch GitHub repo creation)
- `scripts/migrate-skills/cmd/gen-mapping-table/main.go` — NEW (writes docs mapping table from migration metadata)
- `scripts/migrate-skills/release-with-sign.yml.tmpl` — NEW (workflow template committed to each migrated repo)
- `samuel-starter/` — NEW meta-plugin repo
- `internal/plugin/service/install.go` — extend with `meta` kind + dropped-skill error
- `internal/commands/init.go` — `--no-starter` flag
- `samuel-registry/index.toml` — regenerate after migration
- `e2e/live/migration_live_test.go` — NEW
- `docs/getting-started/migration-v1.md` — extend with mapping table
- `docs/blog/v2.3-migration-complete.md` — NEW announcement
- `CHANGELOG.md` — v2.3.x rolling entry

## Tasks

- [ ] 1.0 Confirm bucket assignments [~2,000 tokens - Simple]
  - [ ] 1.1 Cross-reference `wiki/sources/2026-05-12-v1-skill-content-survey.md` against `samuel_v1/.claude/skills/`; verify 4+12+58+2 = 76 (with 2 reserved for "drop" bucket)
  - [ ] 1.2 Document final per-skill bucket table at `scripts/migrate-skills/BUCKETS.md`
  - [ ] 1.3 Resolve "Open question": confirm the 12 starter-pack picks (`go-guide`, `python`, `typescript`, `react`, `nextjs`, `claude-code-tips`, `pr-review`, `test-writer`, `commit-message`, `4d-methodology`, `create-rfd`, `progress-log` per PRD)
  - [ ] 1.4 Verify bucket 1 (4 built-ins) are already shipped as Go template strings in v2 binary; no migration action

- [ ] 2.0 migrate-skill script [~4,500 tokens - Medium]
  - [ ] 2.1 Author `scripts/migrate-skills/cmd/migrate-skill/main.go`: reads a v1 skill dir, emits a v2 plugin tarball to staging
  - [ ] 2.2 Generate `samuel-plugin.toml`: `kind = "skill"`, version `1.0.0`, samuel framework `^2.0.0`, capabilities `filesystem.read = ["/workspace"]`, metadata from v1's `[metadata]`
  - [ ] 2.3 Copy `SKILL.md` byte-identical (frontmatter unchanged per agentskills.io standard)
  - [ ] 2.4 Copy `scripts/`, `references/`, `assets/` if present
  - [ ] 2.5 Auto-generate `README.md` (name, summary, origin v1 commit SHA, license, "migrated from v1 via PRD 0011")
  - [ ] 2.6 Copy `LICENSE` from v1 root
  - [ ] 2.7 Generate `CHANGELOG.md`: `## [1.0.0] — initial release (migrated from v1 commit <sha>)`
  - [ ] 2.8 Generate `.gitignore` (standard plugin layout)
  - [ ] 2.9 Idempotent: re-running over same skill produces identical output (stripped metadata timestamps)

- [ ] 3.0 Migration unit tests [~2,000 tokens - Simple]
  - [ ] 3.1 Byte-identity check: SHA256(migrated SKILL.md) == SHA256(v1 SKILL.md)
  - [ ] 3.2 Determinism check: two consecutive runs produce identical output trees
  - [ ] 3.3 Run conversion against 5 representative skills; manually verify output structure
  - [ ] 3.4 Validate generated manifests with `samuel plugin validate`

- [ ] 4.0 provision-repos script [~3,500 tokens - Medium]
  - [ ] 4.1 Author `scripts/migrate-skills/cmd/provision-repos/main.go`: staging directory → GitHub repos
  - [ ] 4.2 Per plugin: `gh repo create samuelpkg/samuel-<name> --public --description=<summary>`
  - [ ] 4.3 Force-push initial commit; set topics from manifest metadata; create `v1.0.0` release tag
  - [ ] 4.4 Rate-limit aware: backoff on 429; batches of 10 with 30s sleeps
  - [ ] 4.5 `--dry-run` mode lists what would be created without making changes
  - [ ] 4.6 Idempotent: re-running detects existing repos; only commits when there's drift; fail-loud on push errors

- [ ] 5.0 Release/signing workflow template [~2,500 tokens - Simple]
  - [ ] 5.1 Author `scripts/migrate-skills/release-with-sign.yml.tmpl`
  - [ ] 5.2 Workflow: validate manifest → tar.gz → cosign sign-blob (keyless OIDC) → GitHub release with bundle
  - [ ] 5.3 Triggered on tag push; pinned action versions
  - [ ] 5.4 Identity pattern matches the v2.1 default: `https://github.com/samuelpkg/samuel-*/...`
  - [ ] 5.5 Provision script commits this workflow to each migrated repo at provision time

- [ ] 6.0 Pure-bucket batch provisioning [~3,000 tokens - Medium]
  - [ ] 6.1 Run migrate-skill against the 58 pure-bucket skills → staging
  - [ ] 6.2 Spot-check 30 random skills (byte-identity + manifest validation); record findings
  - [ ] 6.3 `provision-repos --dry-run` against all 58; resolve any name collisions
  - [ ] 6.4 Provision in 6 batches of ~10 (rate-limit safe)
  - [ ] 6.5 Trigger v1.0.0 release workflow per repo; verify cosign signing succeeds
  - [ ] 6.6 Record any failures + recovery actions in `BUCKETS.md`

- [ ] 7.0 Starter-pack sub-plugins [~2,000 tokens - Simple]
  - [ ] 7.1 Run migrate-skill against the 12 starter-pack skills → staging
  - [ ] 7.2 Provision 12 repos via provision-repos
  - [ ] 7.3 Trigger v1.0.0 releases; verify signing

- [ ] 8.0 samuel-starter meta-plugin [~2,500 tokens - Simple]
  - [ ] 8.1 Create `github.com/samuelpkg/samuel-starter` repo
  - [ ] 8.2 Manifest: `kind = "meta"`, `[dependencies]` listing the 12 sub-plugins by name + version
  - [ ] 8.3 README documents the 12 included plugins + the auto-install-on-init behavior
  - [ ] 8.4 Tag `v1.0.0` (manifest-only; meta-plugin has no payload)
  - [ ] 8.5 Register in `samuel-registry` with `category = "starter"`

- [ ] 9.0 meta kind support in framework [~3,000 tokens - Medium]
  - [ ] 9.1 Extend `internal/plugin/service/install.go` with `meta` kind
  - [ ] 9.2 Transactional install: resolve all deps, install each, rollback all on partial failure
  - [ ] 9.3 Structured error on partial failure lists what failed + recovery hint (`samuel install samuel-starter --retry`)
  - [ ] 9.4 Uninstall removes the meta + all transitively-installed deps as a group
  - [ ] 9.5 Unit tests: success path, partial-failure rollback, retry resumes

- [ ] 10.0 samuel init default behavior [~2,000 tokens - Simple]
  - [ ] 10.1 `samuel init` installs `samuel-starter` by default
  - [ ] 10.2 `samuel init --no-starter` skips
  - [ ] 10.3 `samuel init --starter=<other-pack>` allows custom starter packs (forward-compatible; v2.3 ships only the official)
  - [ ] 10.4 CHANGELOG + `docs/getting-started/quick-start.md` document the default-install
  - [ ] 10.5 Structured-error message in `samuel init` recommends `--no-starter` for CI

- [ ] 11.0 Dropped-skill error path [~1,500 tokens - Simple]
  - [ ] 11.1 In `internal/plugin/service/install.go`, hardcode dropped skill names: `gstack`, `gbrain`
  - [ ] 11.2 `samuel install gstack` returns structured error with DocsURL pointing at migration doc
  - [ ] 11.3 Error message acknowledges drop rationale (link to RFD 0008)
  - [ ] 11.4 Unit test asserts the error message + exit code

- [ ] 12.0 Registry regeneration [~2,000 tokens - Simple]
  - [ ] 12.1 Re-run `scripts/registry-generator/` (existing tool from v2.0) against populated namespace
  - [ ] 12.2 Verify new `index.toml` lists 71 plugins (58 pure + 12 starter sub + 1 meta; 4 built-ins not in registry)
  - [ ] 12.3 Tags per plugin auto-populated from manifest `[metadata]`
  - [ ] 12.4 Commit to `samuel-registry/`; verify CI `validate.yml` passes

- [ ] 13.0 Live e2e migration tests [~2,500 tokens - Simple]
  - [ ] 13.1 `e2e/live/migration_live_test.go`: `TestMigration_InstallsTopFive` — install `go-guide`, `react`, `python`, `typescript`, `create-rfd`; all healthy in `samuel doctor`
  - [ ] 13.2 `TestMigration_StarterPackInstalls` — `samuel init` against fresh tempdir installs `samuel-starter` + 12 deps
  - [ ] 13.3 `TestMigration_StarterPackSkipsWithFlag` — `samuel init --no-starter` produces minimal project
  - [ ] 13.4 `TestMigration_DroppedSkillReturnsError` — `samuel install gstack` returns structured error with DocsURL

- [ ] 14.0 Mapping-table generator + docs [~3,000 tokens - Medium]
  - [ ] 14.1 Author `scripts/migrate-skills/cmd/gen-mapping-table/main.go` — reads migration metadata, writes the v1 → v2 table in `docs/getting-started/migration-v1.md`
  - [ ] 14.2 Programmatic generation so the table can't drift
  - [ ] 14.3 Table columns: v1 skill name | v2 plugin name | bucket | notes
  - [ ] 14.4 Run generator; commit the generated table
  - [ ] 14.5 Update `docs/concepts/registry.md` reflecting the populated state
  - [ ] 14.6 Link from `docs/plugin-authors/` to one migrated plugin as a real-world example

- [ ] 15.0 v2.3-migration-complete blog post [~2,000 tokens - Simple]
  - [ ] 15.1 Draft `docs/blog/v2.3-migration-complete.md`
  - [ ] 15.2 Cover: rationale, bucket split, mechanical conversion, registry now-populated, what's next
  - [ ] 15.3 Cross-link to migration-v1.md and the mapping table
  - [ ] 15.4 Acknowledge dropped skills with link to RFD 0008

- [ ] 16.0 v1 archive [~2,000 tokens - Simple]
  - [ ] 16.1 Archive `github.com/samuelpkg/samuel-v1` (or whatever v1 repo) via GitHub UI / `gh repo archive`
  - [ ] 16.2 Rewrite v1 README pointing at v2 + the migrated plugin namespace
  - [ ] 16.3 Preserve `v1-final` tag (already created in PRD 0006); no force-push beyond what's already done
  - [ ] 16.4 Verify `v1-final` tag resolves via `git fetch --tags && git checkout v1-final`
  - [ ] 16.5 Migration doc explicitly invites issues against the v2 plugin repos directly

- [ ] 17.0 Release v2.3.x rolling [~1,500 tokens - Simple]
  - [ ] 17.1 CHANGELOG entry summarizes the migration (count of plugins, link to mapping table)
  - [ ] 17.2 Tag `v2.3.1` (or whatever patch the migration lands in)
  - [ ] 17.3 Announce: cross-link blog post + migration table
  - [ ] 17.4 Open follow-up issues for any plugins discovered to need rewrite post-migration (separate from this PRD)
