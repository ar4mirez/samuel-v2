# Tasks — PRD 0006: Samuel v2 Polish + Launch

> Generated from [0006-prd-polish-launch.md](0006-prd-polish-launch.md) on 2026-05-12.
> Depends on PRD 0005 (Skill Migration) being complete.

## Relevant files

- All 8 RFDs (already written and Committed)
- `.wiki/synthesis/v2-template-and-docs.md` — docs restructure plan
- `samuel_v1/mkdocs.yml`, `samuel_v1/docs/` — source for port
- `samuel_v1/CHANGELOG.md` v3.0.0 entry — quality bar to match

## Tasks

- [ ] 1.0 Charm UI swap [~5,000 tokens - Medium]
  - [ ] 1.1 Pin `charmbracelet/lipgloss`, `charmbracelet/huh`, `charmbracelet/bubbles` versions in go.mod
  - [ ] 1.2 Swap `fatih/color` → `lipgloss` throughout `internal/ui/output.go`; keep same six-category vocabulary
  - [ ] 1.3 Swap `manifoldco/promptui` → `huh` throughout `internal/ui/prompts.go`; native multi-select replaces v1's custom toggle impl
  - [ ] 1.4 Swap `schollz/progressbar/v3` → `charmbracelet/bubbles/{spinner,progress}` throughout `internal/ui/spinner.go`
  - [ ] 1.5 Regression test: every UI helper produces equivalent output / behavior to pre-swap version
  - [ ] 1.6 Update `--no-color` to respect lipgloss's `Renderer.SetColorProfile(termenv.Ascii)`

- [ ] 2.0 AGENTS.md template final polish [~2,500 tokens - Simple]
  - [ ] 2.1 Render AGENTS.md against max-config samuel.toml fixture
  - [ ] 2.2 Measure rendered line count; should be ≤150
  - [ ] 2.3 If exceeded, trim non-essential content per RFD 0002 resolution #5 priority order
  - [ ] 2.4 Run `agents-md-check.yml` CI locally; verify it would catch a +151 violation

- [ ] 3.0 Agnostic-by-design CI check [~1,500 tokens - Simple]
  - [ ] 3.1 Author `.github/workflows/agnostic-check.yml` per RFD 0002 spec
  - [ ] 3.2 Grep checks: `"CLAUDE\.md"` literal and `"\.claude/"` path in `internal/`, `cmd/`, `template/`
  - [ ] 3.3 Run against current codebase — should pass
  - [ ] 3.4 Plant intentional violation in a test commit; verify CI catches; revert
  - [ ] 3.5 Document override pattern for future legitimate needs (none expected; flag in PR if needed)

- [ ] 4.0 Docs site restructure [~7,000 tokens - Complex]
  - [ ] 4.1 Port `mkdocs.yml` from v1; update site_name, repo_url, edit_uri to v2 paths
  - [ ] 4.2 Author `docs/index.md` — landing page
  - [ ] 4.3 Author `docs/core/overview.md` (architecture diagram + key components)
  - [ ] 4.4 Author `docs/core/agents-md.md` (replaces v1's claude-md.md)
  - [ ] 4.5 Author `docs/core/methodology.md` (4D + Ralph)
  - [ ] 4.6 Author `docs/core/guardrails.md` (default ruleset + sourcing from samuel.toml)
  - [ ] 4.7 Author `docs/core/plugins.md` (three tiers + lifecycle)
  - [ ] 4.8 Author `docs/getting-started/installation.md` (brew/curl/go-install)
  - [ ] 4.9 Author `docs/getting-started/quick-start.md` (60-second walkthrough)
  - [ ] 4.10 Author `docs/getting-started/first-task.md` (PRD → run loop)
  - [ ] 4.11 Author `docs/getting-started/migration-v1.md` (what changed; not an upgrade tool)
  - [ ] 4.12 Author `docs/reference/cli.md` (every command + flags + JSON envelope)
  - [ ] 4.13 Author `docs/reference/faq.md`
  - [ ] 4.14 Author `docs/reference/cross-tool.md` (translator plugin behavior)
  - [ ] 4.15 Author `docs/reference/changelog.md` (link to repo CHANGELOG)
  - [ ] 4.16 Port `docs/concepts/` — bring forward the wiki concepts most relevant to users (plugin-format, agents-md-primary, agnostic-by-design, methodology-hooks, 4d-methodology, ralph-wiggum-methodology)
  - [ ] 4.17 Author `docs/plugin-authors/` — manifest reference, hooks guide, capability model, TinyGo WASM toolchain, OCI gRPC bindings, signing workflow
  - [ ] 4.18 Author `docs/plugins/` generator script — pulls each plugin's README from `samuel-registry/index.toml`; runs at docs build time
  - [ ] 4.19 Update navigation in `mkdocs.yml`
  - [ ] 4.20 Run `mkdocs build --strict` clean

- [ ] 5.0 RFDs published in docs [~1,500 tokens - Simple]
  - [ ] 5.1 RFDs already at `docs/rfd/0001-0008.md` (written, Committed)
  - [ ] 5.2 Add `docs/rfd/index.md` listing all RFDs (state, labels)
  - [ ] 5.3 Generator script from `rfd-index.toml` to keep the index page fresh
  - [ ] 5.4 Ensure mkdocs nav includes RFDs section

- [ ] 6.0 CHANGELOG v2.0.0 entry [~3,000 tokens - Medium]
  - [ ] 6.1 Create `CHANGELOG.md` at repo root with Keep a Changelog format
  - [ ] 6.2 Write v2.0.0 entry matching v1 v3.0.0 quality — thesis upfront, rationale per item, internal section with filenames, test stats
  - [ ] 6.3 Sections: Added, Changed, Deprecated (none — clean break), Removed (gstack/gbrain), Internal, Migration
  - [ ] 6.4 Link from README

- [ ] 7.0 Release candidate sequence — v2.0.0-rc.2 [~2,000 tokens - Simple]
  - [ ] 7.1 Verify all milestone PRDs land
  - [ ] 7.2 Verify all CI gates green (agents-md-check, agnostic-check, plugin compatibility check)
  - [ ] 7.3 Update CHANGELOG with rc.2 notes
  - [ ] 7.4 Tag `v2.0.0-rc.2`; goreleaser publishes signed artifacts
  - [ ] 7.5 Announce rc.2; collect feedback

- [ ] 8.0 Bug-fix soak — 1 week [~3,000 tokens - Medium]
  - [ ] 8.1 Triage rc.2 feedback; categorize bugs (blocker / serious / nice-to-have)
  - [ ] 8.2 Fix blockers + serious
  - [ ] 8.3 Update CHANGELOG with rc.3 notes
  - [ ] 8.4 Tag `v2.0.0-rc.3`

- [ ] 9.0 Final soak — 1 week [~1,500 tokens - Simple]
  - [ ] 9.1 Triage rc.3 feedback
  - [ ] 9.2 Final fixes
  - [ ] 9.3 Update CHANGELOG with v2.0.0 notes (consolidated from rc series)

- [ ] 10.0 v1 deprecation prep [~3,000 tokens - Medium]
  - [ ] 10.1 Tag `v1-final` at the last v1 commit in `github.com/samuelpkg/samuel` (preserves access via tag)
  - [ ] 10.2 Draft replacement README pointing to v2 with prominent v1-final tag link
  - [ ] 10.3 Verify install.sh script works from clean state
  - [ ] 10.4 Verify Homebrew tap formula update path

- [ ] 11.0 v2.0.0 ship [~2,500 tokens - Medium]
  - [ ] 11.1 Final CHANGELOG cleanup
  - [ ] 11.2 Tag `v2.0.0` (signed); push
  - [ ] 11.3 Force-push `github.com/samuelpkg/samuel` `main` to v2 code (after `v1-final` tag is in place)
  - [ ] 11.4 Update Homebrew tap formula; verify `brew install samuel` lands v2.0.0
  - [ ] 11.5 Verify `curl -sSL <install.sh> | sh` installs v2.0.0 on clean macOS + Linux containers
  - [ ] 11.6 Trigger docs deploy to ar4mirez.github.io/samuel/
  - [ ] 11.7 Smoke test: `samuel init my-project` end-to-end in fresh container

- [ ] 12.0 Announcement [~2,500 tokens - Medium]
  - [ ] 12.1 Draft blog post at `docs/blog/samuel-v2-launch.md` (or external)
  - [ ] 12.2 Sections: rebuild rationale, plugin architecture (cite RFD 0001), AGENTS.md primary (cite RFD 0002), agnostic-by-design (cite RFD 0002), Ralph preserved (cite RFD 0006), TOON runtime (cite RFD 0006), what's new for users
  - [ ] 12.3 Acknowledge gstack/gbrain drop with rationale (link to RFD 0008)
  - [ ] 12.4 Publish to chosen channel
  - [ ] 12.5 Post on relevant communities (HN, lobste.rs, etc.) if comfortable

- [ ] 13.0 Post-launch follow-ups (track separately) [~500 tokens - Simple]
  - [ ] 13.1 Open GitHub issues for known v2.1 candidates: `samuel plugin new/build/publish`, additional translator plugins (cursor, continue, aider), Sigstore signed-default, hot-reload design
  - [ ] 13.2 Track community plugin authoring; help where useful
  - [ ] 13.3 Schedule v2.0.x patch release cadence (monthly initially)
