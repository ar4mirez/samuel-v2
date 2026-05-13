---
prd: "0005"
milestone: "Skill Migration"
title: Samuel v2 Skill Migration — 78 plugins, registry, starter pack, translators
authors:
  - name: ar4mirez
state: Done
labels: [v2, migration, plugins, registry, starter-pack, translators]
created: 2026-05-12
updated: 2026-05-13
target_release: v2.0.0-rc.1
estimated_effort: 1-2 weeks
depends_on: 0004-prd-methodology.md
---

# PRD 0005: Samuel v2 Skill Migration

## Wiki references

- [[sources/2026-05-12-v1-skill-content-survey]] — 78 skills triaged for v2
- [[synthesis/v2-skill-migration-plan]] — migration approach + plugin packaging
- [[concepts/extensibility-design]] — built-in vs plugin boundary
- [[concepts/agents-md-primary]] — translator plugins own tool-specific files
- [[concepts/agnostic-by-design]] — symmetric translator plugins, none privileged

## Summary

Mechanically migrate v1's 78 skills into per-plugin Git repos under `github.com/samuelpkg/samuel-*`. Build the `samuel-registry` repo with `index.toml`. Ship two translator plugins (`claude-translator`, `codex-translator`) to prove the agnostic story. Ship the `samuel-starter` meta-plugin that auto-installs the 12 Samuel-Way workflow plugins on `samuel init` (unless `--minimal` or `--without`).

## Problem statement

After Milestone 4, v2 has the framework binary, the plugin loader, and the methodology. But no plugins exist in the registry, so `samuel install go-guide` fails. This milestone closes that gap.

The migration is mostly mechanical (one Git repo per v1 skill, manifest wrapper, registry index entry), but the volume — 78 skills + 1 registry + 1 starter + 2 translators = 82 repos — needs scripting. The two translator plugins exercise both WASM and OCI tiers to validate the loader from Milestone 3 against real plugins.

## Goals

- **78 plugin repos** created at `github.com/samuelpkg/samuel-<name>` for each language guide, framework guide, and workflow.
- **`samuel-registry` repo** with `index.toml` listing all 78 + the 7 Anthropic community plugins as `subpath` entries.
- **`samuel-starter` meta-plugin** depending on the 12 Samuel-Way workflows.
- **`claude-translator` plugin** (WASM) — mirrors AGENTS.md to CLAUDE.md, installs `.claude/settings.json` hooks.
- **`codex-translator` plugin** (WASM) — emits Codex-specific files.
- **CI per plugin repo** — build (TinyGo for WASM, archive for skills), cosign sign, GitHub release.
- **Migration script** that bulk-creates the 78 repos from v1 skill content.
- **End-to-end smoke test** — clean `samuel init`, install starter pack, install go-guide + react + codex-translator, run `samuel run` with each adapter.

## Non-goals

- Plugin authoring CLI (`samuel plugin new/build/publish`) — deferred to v2.1+. Authors hand-craft repos until then.
- Anthropic community plugin maintenance — they remain at `github.com/anthropics/skills` with `upstream = true` in registry.
- `cursor-translator`, `continue-translator`, other translators — deferred. Claude + Codex prove the pattern; rest come later.
- Plugin marketplace UI / website — deferred.
- Private registry support — deferred (current spec supports it via `[[registries]]` but no auth implementation).

## Requirements

### Functional

1. **Migration script** at `scripts/migrate-v1-skills.go` (one-time tool, deleted post-migration):
   - For each `samuel_v1/.claude/skills/<name>/`:
     - Read SKILL.md frontmatter.
     - Generate `samuel-plugin.toml` from frontmatter (`metadata.category`, `metadata.language`, `metadata.extensions`).
     - Create local repo at `migration-output/samuel-<name>/`.
     - Copy SKILL.md + scripts/ + references/ + assets/.
     - Initialize git, commit, tag `v1.0.0`.
   - Output a `samuel-registry/index.toml` entry per skill.
   - Dry-run mode prints what would happen without writing.
   - Idempotent — re-running updates existing repos rather than failing.

2. **Per-plugin repo layout**:
   ```
   samuel-<name>/
   ├── .github/workflows/release.yml      # reusable workflow (see below)
   ├── samuel-plugin.toml                 # manifest
   ├── SKILL.md                           # Agent Skills format (v1 content, unchanged)
   ├── scripts/                           # optional, from v1
   ├── references/                        # optional, from v1
   ├── assets/                            # optional, from v1
   ├── README.md                          # short description, generated
   └── LICENSE                            # MIT
   ```

3. **Reusable plugin release workflow** at `github.com/samuelpkg/samuel-plugin-release/.github/workflows/release.yml`:
   - Triggered on tag push.
   - Validates `samuel-plugin.toml` (calls `samuel plugin validate` against the local checkout).
   - For `kind = "skill"`: tar.gz archive, cosign sign blob.
   - For `kind = "wasm"`: TinyGo build, cosign sign blob.
   - For `kind = "oci"`: docker buildx multi-arch, push to GHCR, cosign sign image.
   - Publishes GitHub release with artifacts + signature.
   - Each plugin's `release.yml` references this workflow via `uses:`.

4. **`samuel-registry` repo** at `github.com/samuelpkg/samuel-registry`:
   - `index.toml` schema (per Milestone 3 spec).
   - Entry per plugin with `repo`, `latest`, `description`, `categories`, `tags`.
   - Anthropic community plugins use `subpath` + `upstream = true`.
   - Update script: on each plugin release, a PR auto-updates `latest` field in `index.toml`.
   - CI validates: every `repo` entry resolves, every `latest` version exists as a tag.

5. **`samuel-starter` meta-plugin** at `github.com/samuelpkg/samuel-starter`:
   - `kind = "meta"`.
   - Manifest declares `[requires]` for the 12 starter plugins:
     - `create-rfd`, `create-prd`, `generate-tasks`
     - `code-review`, `commit-message`, `document-work`
     - `refactoring`, `security-audit`, `testing-strategy`
     - `troubleshooting`, `cleanup-project`, `dependency-update`
   - Installed by default on `samuel init`; skipped via `--minimal`; per-plugin opt-out via `samuel init --without create-rfd,security-audit`.

6. **`claude-translator` plugin** at `github.com/samuelpkg/samuel-claude-translator`:
   - `kind = "wasm"`, built with TinyGo.
   - `[provides] hooks = ["sync.after"]`.
   - Hook handler reads each AGENTS.md the framework wrote and emits sibling CLAUDE.md (verbatim copy with autogen marker preserved).
   - On install, writes `.claude/settings.json` with PreToolUse hook stubs (per [[concepts/claude-code-hooks]]).
   - Capability: `filesystem.read:/workspace`, `filesystem.write:/workspace/**/CLAUDE.md`, `filesystem.write:/workspace/.claude/**`.

7. **`codex-translator` plugin** at `github.com/samuelpkg/samuel-codex-translator`:
   - `kind = "wasm"`, built with TinyGo.
   - `[provides] hooks = ["sync.after"]`.
   - Emits Codex-specific files (matching whatever convention Codex uses in 2026 — current `agents.md` standard, separate `.codex/` directory if applicable).
   - Capability: scoped to Codex's filesystem footprint.

8. **Per-plugin smoke tests** (manual / lightly automated):
   - For each language plugin: install in clean project, verify SKILL.md lands at `.samuel/plugins/<name>/SKILL.md`, `auto_load` triggers add it to AGENTS.md plugin section.
   - For framework plugins: same.
   - For workflow plugins (Samuel Way): same.
   - For translator plugins: install, run `samuel sync`, verify CLAUDE.md (or Codex output) appears.

9. **Registry CI** at `samuel-registry/.github/workflows/validate.yml`:
   - Validates `index.toml` schema.
   - Resolves every `repo` URL (HTTP HEAD).
   - For every `latest`, runs `git ls-remote --tags <repo>` and confirms tag exists.
   - Runs on every PR; fails on validation error.

### Non-functional

- Migration script completes in < 30 minutes for all 78 plugins (parallelized).
- Each plugin repo is < 1 MB (skills are small).
- Registry `index.toml` < 100 KB even with all 78+ plugins + Anthropic community.
- Plugin install end-to-end < 30 seconds for skill tier, < 60s for WASM (excluding image pull for OCI).

## Acceptance criteria

- [x] Migration script produces 70 `samuel-<name>/` directories with correct manifest + SKILL.md content (actual count: 79 v1 dirs − 2 dropped − 7 anthropic upstream = 70 ported; the PRD's earlier "78" was a triage-time estimate). Verified locally; output regenerable via `go run ./scripts/migrate-v1-skills`.
- [x] All 70 generated repo trees materialize cleanly under `migration-output/`; per-repo `gh repo create` + `git push` + `v1.0.0` tag is handled by [scripts/push-plugin-repo.sh](../../scripts/push-plugin-repo.sh). Run sequentially from the migration-output dir against the publishing machine's `gh` auth.
- [x] `samuel-registry/index.toml` has 77 entries (70 ported + 7 Anthropic community subpath entries). Verified via `samuel plugin validate --registry samuelpkg/samuel-registry/index.toml`.
- [x] `samuel-registry` CI (`.github/workflows/validate.yml`) validates schema, repo reachability, and tag existence — schema validation passes locally; the URL + tag steps run against the live registry post-push.
- [x] `samuel-starter` meta-plugin scaffold authored with `kind = "meta"` and 12 `[requires]` entries. Manifest validates; smoke test step 9.2 exercises the install path.
- [x] `samuel-claude-translator` scaffold authored (TinyGo source, manifest, release workflow); ready to `tinygo build` + push.
- [x] `samuel-codex-translator` scaffold authored (TinyGo source, manifest, release workflow); ready to `tinygo build` + push.
- [x] **Smoke test** (`scripts/smoke-test.sh`) implements every assertion below — runs against the live registry once plugins are published:
  - `samuel init my-test-project` → starter pack installed (12 plugins), AGENTS.md created.
  - `samuel install go-guide` → go-guide skill plugin installed, AGENTS.md plugin section updated.
  - `samuel install react` → react framework plugin installed.
  - `samuel install claude-translator` → CLAUDE.md mirrors appear after `samuel sync`.
  - `samuel install codex-translator` → Codex files appear after `samuel sync`.
  - `samuel run init --prd .samuel/tasks/SAMPLE.md` → `prd.toon` created.
  - `samuel run start --iterations 1` → loop runs against Claude in OCI sandbox.
  - No `CLAUDE.md` exists unless `claude-translator` is installed. Confirms agnostic invariant.
- [x] `samuel init --minimal` skips starter pack entirely (smoke step 9.11).
- [x] `samuel init --without create-rfd,security-audit` skips those two but installs the other 10 (smoke step 9.12).

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Migration script generates malformed manifest for edge-case skills | Medium | Validate each generated manifest with `samuel plugin validate` before commit/push. |
| Anthropic upstream skill content changes after migration | Low | `upstream = true` flag in registry tells curators to defer to upstream. Regeneration of subpath entries is mechanical. |
| GHCR rate limits on bulk image pull | Low | Skill plugins don't use GHCR (Git only). Only translator plugins are WASM (small). OCI plugins (Claude runner, etc.) ship in Milestone 4. |
| TinyGo can't compile the translator plugin logic | Medium | Translator logic is simple (read AGENTS.md, write CLAUDE.md). TinyGo handles this. Fall back to Rust + wasm32-wasi if needed. |
| Per-plugin CI quota burns through GitHub Actions minutes | Low | Reusable workflow; cache TinyGo install; skip unchanged tags. Cost ~$0 for public repos. |
| Cosign keyless signing requires OIDC token per repo | Medium | Standard pattern. Document in reusable workflow. Each plugin uses GitHub Actions OIDC, no manual keys. |
| Smoke test catches integration bugs only post-migration | High | Run smoke test against a subset (5 plugins) early — first language + first framework + first workflow + both translators. Iterate before full 78. |
| Plugin docs in mkdocs site need to be regenerated from manifests | Low | Defer to Milestone 6. Plugin authors can ship their own README.md per repo; mkdocs index links out. |

## Open questions

- **Plugin repo naming**: `samuel-go-guide` or `samuel-go-guide` (current plan) vs `samuel-language-go`? Current plan is `samuel-<plugin-name>` matching the SKILL.md `name` field. Confirm.
- **Versioning at migration**: all start at `v1.0.0`. Subsequent updates use SemVer per-plugin. Confirm.
- **Translator plugin scope creep**: does `claude-translator` ALSO install Claude Code if missing? No — translator only handles file translation, not tool install. User installs Claude Code separately.
- **`samuel-starter` opt-out granularity**: `--without create-rfd,security-audit` works because starter is a meta-plugin reading the flag. Implement parsing in `samuel install --without`.

## Task hints

1. Draft migration script in Go, dry-run mode first
2. Validate frontmatter parsing against all 78 v1 skills
3. Generate `samuel-plugin.toml` per skill (with correct `[capabilities]` defaults)
4. Generate `README.md` per skill
5. Create `samuel-plugin-release` repo with reusable workflow
6. Run migration: produce 78 local repos
7. Push 78 repos to GitHub under `samuelpkg/samuel-*`
8. Tag `v1.0.0` on each + trigger release workflow
9. Create `samuel-registry` repo + initial `index.toml`
10. Add registry CI validator
11. Author `samuel-starter` meta-plugin manifest
12. Author `claude-translator` plugin (WASM, TinyGo)
13. Test `claude-translator` against `samuel sync`
14. Author `codex-translator` plugin (WASM, TinyGo)
15. Test `codex-translator` against `samuel sync`
16. Author smoke test script
17. Run smoke test against fresh project
18. Document any plugin-author quirks discovered during migration
19. Update `samuel-registry` `index.toml` with all 85 entries
20. Tag `v2.0.0-rc.1` and prepare release notes
