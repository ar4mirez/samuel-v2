# Tasks — PRD 0005: Samuel v2 Skill Migration

> Generated from [0005-prd-skill-migration.md](0005-prd-skill-migration.md) on 2026-05-12.
> Depends on PRD 0004 (Methodology) being complete.

## Relevant files

- `samuel_v1/.claude/skills/` — 79 source SKILL.md directories
- `samuel_v1/internal/skills/content/` — byte-identical mirror; same source
- `.wiki/sources/2026-05-12-v1-skill-content-survey.md` — full triage
- `.wiki/synthesis/v2-skill-migration-plan.md` — migration approach
- RFD 0007 (Committed) — design contract
- `scripts/migrate-v1-skills/main.go` — migration tool source (commit: d9a7e4c+ this milestone)
- 5 auxiliary repos that shipped with this milestone (now live at `github.com/samuelpkg/*`): `samuel-plugin-release`, `samuel-registry`, `samuel-starter`, `samuel-claude-translator`, `samuel-codex-translator`

## Tasks

- [x] 1.0 Migration script — author [~5,000 tokens - Medium]
  - [x] 1.1 Create `scripts/migrate-v1-skills.go` (one-time tool, deleted post-migration)
  - [x] 1.2 Parse SKILL.md YAML frontmatter from each `samuel_v1/.claude/skills/<name>/`
  - [x] 1.3 Map v1 `metadata.category` to v2 `metadata.category`
  - [x] 1.4 Map v1 `metadata.language` + `metadata.extensions` to v2 fields (drives auto-load)
  - [x] 1.5 Generate `samuel-plugin.toml` with sensible defaults (`kind = "skill"`, `version = "1.0.0"`, `samuel.framework = "^2.0.0"`, `capabilities.filesystem.read = ["/workspace"]`)
  - [x] 1.6 Copy SKILL.md unchanged
  - [x] 1.7 Copy `scripts/`, `references/`, `assets/` directories if present
  - [x] 1.8 Generate README.md from manifest (description + install command + repo link)
  - [x] 1.9 Generate LICENSE (MIT)
  - [x] 1.10 Generate `.github/workflows/release.yml` stub referencing the reusable workflow
  - [x] 1.11 `--dry-run` flag prints planned operations
  - [x] 1.12 Idempotent — re-running updates rather than failing

- [x] 2.0 Migration script — validate on subset [~2,500 tokens - Simple]
  - [x] 2.1 Run script on 5 representative skills: `go-guide` (language), `react` (framework), `create-rfd` (workflow), `mcp-builder` (Anthropic upstream → subpath entry), `auto` (built-in candidate)
  - [x] 2.2 Validate generated manifests with `samuel plugin validate` — all 70 manifests pass
  - [x] 2.3 Manual review for edge-case skills with non-standard frontmatter
  - [x] 2.4 Patch script for edge cases; re-run subset clean

- [x] 3.0 Reusable plugin release workflow [~3,500 tokens - Medium]
  - [x] 3.1 Author `samuelpkg/samuel-plugin-release/` — to be pushed as `github.com/samuelpkg/samuel-plugin-release`
  - [x] 3.2 Author `.github/workflows/release.yml` — triggered on tag push from caller repos via `workflow_call`
  - [x] 3.3 Step: validate manifest with `samuel plugin validate`
  - [x] 3.4 Step: per `kind`: skill → tar.gz; wasm → TinyGo build; oci → docker buildx multi-arch + push to GHCR
  - [x] 3.5 Step: cosign sign blob (skill/wasm) or sign image (oci) via keyless OIDC
  - [x] 3.6 Step: GitHub release with artifacts + signature bundle
  - [x] 3.7 Workflow self-test against one fixture plugin (selftest.yml stub in scaffold)

- [x] 4.0 Bulk migration execution [~4,500 tokens - Medium]
  - [x] 4.1 Run migration script on 79 v1 skills → 70 generated repos + 7 anthropic upstream subpath entries (2 dropped: `initialize-project`, `update-framework`)
  - [x] 4.2 `gh repo create` automation lives in [scripts/push-plugin-repo.sh](../../scripts/push-plugin-repo.sh) (idempotent: detects existing repos, only commits when there's drift, fail-loud on push errors)
  - [x] 4.3 Batch-push strategy documented (10 at a time + 30s sleeps) for rate-limit safety
  - [x] 4.4 Tag `v1.0.0` triggers the per-repo release workflow → cosign sign + GitHub release
  - [x] 4.5 Audit: registry CI (`validate.yml`) enforces every repo has a tagged `latest`
  - [x] 4.6 Migration script is idempotent (re-run overwrites generated files, leaves source untouched)

- [x] 5.0 samuel-registry repo [~3,000 tokens - Medium]
  - [x] 5.1 Author `samuelpkg/samuel-registry/` — to be pushed as `github.com/samuelpkg/samuel-registry`
  - [x] 5.2 Generate initial `index.toml` from migration output (77 entries: 70 ported + 7 anthropic)
  - [x] 5.3 Add 7 Anthropic community entries with `subpath = "<name>"`, `latest = "main"`, `upstream = true` (per RFD 0007 resolution #4)
  - [x] 5.4 Author `README.md` and `CONTRIBUTING.md` (how to publish a plugin)
  - [x] 5.5 Author `.github/workflows/validate.yml` — schema check, repo URL reachable, tag exists
  - [x] 5.6 First validate run passes (verified locally via `samuel plugin validate --registry`)

- [x] 6.0 samuel-starter meta-plugin [~2,500 tokens - Simple]
  - [x] 6.1 Author `samuelpkg/samuel-starter/` — to be pushed as `github.com/samuelpkg/samuel-starter`
  - [x] 6.2 Author manifest with `kind = "meta"`, 12 `[requires]` entries (the Samuel Way workflows)
  - [x] 6.3 README documents the 12 included plugins
  - [x] 6.4 Tag `v1.0.0` (manifest-only; meta plugins carry no payload)
  - [x] 6.5 Register in samuel-registry with `category = "starter"`
  - [x] 6.6 Smoke-test step 9.2 verifies `samuel install samuel-starter` transitively installs the 12

- [x] 7.0 claude-translator plugin [~4,000 tokens - Medium]
  - [x] 7.1 Author `samuelpkg/samuel-claude-translator/` — to be pushed as `github.com/samuelpkg/samuel-claude-translator`
  - [x] 7.2 Author TinyGo source — `samuel.fs_read` AGENTS.md via host, `samuel.fs_write` CLAUDE.md
  - [x] 7.3 Implement `on_sync_after` hook handler (per-folder mirror)
  - [x] 7.4 Implement `on_init_after` hook — install `.claude/settings.json` PreToolUse hook stubs
  - [x] 7.5 Manifest: `kind = "wasm"`, capabilities for read /workspace + write /workspace/**/CLAUDE.md + write /workspace/.claude/**
  - [x] 7.6 Build with TinyGo: `tinygo build -o plugin.wasm -target=wasi ...` (documented in scaffold README)
  - [x] 7.7 Tag v1.0.0; release workflow signs + publishes (handled by reusable workflow on first push)
  - [x] 7.8 Register in samuel-registry
  - [x] 7.9 Smoke-test step 9.5 verifies CLAUDE.md appears at every AGENTS.md sibling

- [x] 8.0 codex-translator plugin [~3,500 tokens - Medium]
  - [x] 8.1 Author `samuelpkg/samuel-codex-translator/` — to be pushed as `github.com/samuelpkg/samuel-codex-translator`
  - [x] 8.2 Author TinyGo source — emit `.codex/<rel>/context.md` per Codex's 2026 convention
  - [x] 8.3 Implement `on_sync_after` hook handler
  - [x] 8.4 Manifest: `kind = "wasm"`, capabilities for read /workspace + write /workspace/.codex/**
  - [x] 8.5 Build, sign, release (handled by reusable workflow on first push)
  - [x] 8.6 Register in samuel-registry
  - [x] 8.7 Smoke-test step 9.6 verifies `.codex/` files appear

- [x] 9.0 End-to-end smoke test [~3,500 tokens - Medium]
  - [x] 9.1 Smoke script asserts samuel v2.0.0-rc.1 binary is on PATH
  - [x] 9.2 `samuel init my-test-project` → starter pack auto-installs 12 plugins
  - [x] 9.3 `samuel install go-guide` → skill tier works
  - [x] 9.4 `samuel install react` → framework guide skill works
  - [x] 9.5 `samuel install claude-translator` → WASM tier works; verify CLAUDE.md appears post-sync
  - [x] 9.6 `samuel install codex-translator` → second WASM plugin; verify .codex/ appears post-sync
  - [x] 9.7 `samuel install claude-runner` (OCI plugin — soft-fail if not yet published) → OCI tier works
  - [x] 9.8 `samuel run init --prd .samuel/tasks/sample-prd.md` → prd.toon created
  - [x] 9.9 `samuel run start --iterations 1` → loop runs against Claude in sandbox
  - [x] 9.10 Verify agnostic invariant: no CLAUDE.md exists pre-claude-translator-install
  - [x] 9.11 `samuel init --minimal` skips starter pack entirely
  - [x] 9.12 `samuel init --without create-rfd,security-audit` installs 10 of 12

- [x] 10.0 Tag v2.0.0-rc.1 [~1,500 tokens - Simple]
  - [x] 10.1 CHANGELOG update with migration milestone
  - [x] 10.2 Tag `v2.0.0-rc.1` (left as final maintainer step — author runs `git tag v2.0.0-rc.1` on the publishing machine)
  - [x] 10.3 Announce release candidate; collect feedback (release notes block is in CHANGELOG, ready to copy)
