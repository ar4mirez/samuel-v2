---
title: Samuel Wiki Index
type: index
created: 2026-05-12
updated: 2026-05-12
---

# Samuel Wiki

Knowledge base for the Samuel v2 rebuild. v1 lives in `samuel_v1/` as reference. v2 lives in `samuel/`.

See [[CLAUDE]] for conventions.

## Sources

- [[sources/2026-05-12-v1-skill-model]] — Pass 1: embed.go, skills/README.md, core/skill.go, core/registry.go
- [[sources/2026-05-12-v1-config-sync]] — Pass 2: config.go, sync.go, downloader.go, extractor.go, docker.go, samuel.yaml
- [[sources/2026-05-12-v1-auto-mode]] — Pass 3: auto.go + 10 companion files (the autonomous loop)
- [[sources/2026-05-12-v1-orchestrator]] — Pass 4: orchestrator package (lifecycle + rollback)
- [[sources/2026-05-12-v1-github-ui]] — Pass 5: github client + ui package
- [[sources/2026-05-12-v1-commands]] — Pass 6: 33-file commands layer (the full CLI surface)
- [[sources/2026-05-12-v1-cli-entry-runtime]] — Pass 7: cmd/samuel/main.go + .claude/ runtime
- [[sources/2026-05-12-v1-skill-content-survey]] — Pass 8: 78 skills catalogued + triaged
- [[sources/2026-05-12-v1-template-docs]] — Pass 9: template/ + docs/ + mkdocs.yml
- [[sources/2026-05-12-v1-rfds]] — Pass 10: four committed RFDs + the RFD process
- [[sources/2026-05-12-v1-build-release]] — Pass 11: Makefile + goreleaser + CI/release + install.sh
- [[sources/2026-05-12-v1-project-meta]] — Pass 12: README, root CLAUDE.md/AGENTS.md, CHANGELOG (12 releases)

## Entities

- [[entities/samuel-v1]] — the current shipped codebase (anchor)
- [[entities/samuel-v2]] — the rebuild target (anchor)
- [[entities/skill-md]] — SKILL.md file format and validation
- [[entities/skills-embed]] — go:embed binary skill bundling
- [[entities/registry]] — core/registry.go static component catalog `#drop`
- [[entities/config-go]] — core/config.go + samuel.yaml schema `#drop`
- [[entities/sync-claude-md]] — per-folder CLAUDE.md/AGENTS.md generator `#rescue`
- [[entities/downloader-extractor]] — tarball fetch + file extractor (v3-era flow)
- [[entities/docker-sandbox]] — v1's multi-agent sandbox layer `#rescue`
- [[entities/auto-prd]] — prd.json data model `#rescue`
- [[entities/auto-loop]] — RunAutoLoop + InvokeAgent `#rescue`
- [[entities/auto-runtime-files]] — the seven `.claude/auto/` files
- [[entities/auto-prompts]] — implementation + discovery prompt templates
- [[entities/orchestrator]] — Orchestrator + Component interface + lock + Error `#rescue`
- [[entities/component-samuel]] — embedded-skill sync component `#rescue`
- [[entities/component-gstack-gbrain]] — composed externals `#drop`
- [[entities/github-client]] — GitHub HTTP wrapper `#refactor`
- [[entities/ui-package]] — colored output + JSON envelope + prompts + spinner `#refactor`
- [[entities/command-tree-v1]] — full v1 CLI surface with per-command v2 disposition
- [[entities/config-format]] — TOML primary, YAML supported, SKILL.md frontmatter stays YAML `#v2-decision`

## Concepts

- [[concepts/agent-skills-standard]] — the agentskills.io open standard `#rescue`
- [[concepts/skills-architecture-v1]] — end-to-end view of v1's skill system
- [[concepts/extensibility-design]] — v2 built-in vs plugin boundary `#v2-decision`
- [[concepts/plugin-format]] — v2 plugin format + sandbox recommendation `#v2-decision`
- [[concepts/versioning-compatibility]] — v2 SemVer + capability model recommendation `#v2-decision`
- [[concepts/per-folder-context]] — auto-generated per-dir AGENTS.md `#v2-decision`
- [[concepts/agnostic-by-design]] — cross-tool invariant; nothing in framework assumes a specific AI tool `#v2-decision`
- [[concepts/toon-evaluation]] — TOON format evaluation; encoding-per-file-class decision
- [[concepts/multi-agent-support]] — Samuel as cross-tool agent runner `#v2-decision`
- [[concepts/methodology-default-plus-plugin]] — built-in defaults + plugin enhancement `#v2-decision`
- [[concepts/ralph-wiggum-methodology]] — autonomous coding methodology basis `#rescue`
- [[concepts/pre-computed-context]] — token-discipline pattern `#rescue` (the v1 innovation)
- [[concepts/pilot-mode]] — discovery + implementation alternation `#rescue`
- [[concepts/prompt-template-variables]] — spec for what v2 exposes to prompt templates `#v2-decision`
- [[concepts/component-lifecycle]] — Detect/Install/Check/Uninstall + Mutation log `#rescue` `#v2-decision`
- [[concepts/structured-errors]] — Problem/Cause/Fix/DocsURL `#rescue` `#v2-decision`
- [[concepts/smart-bare-invocation]] — never silently start work `#rescue`
- [[concepts/json-mode-everywhere]] — every command emits `--json` `#rescue` `#v2-decision`
- [[concepts/agents-md-primary]] — AGENTS.md primary, CLAUDE.md via translator plugin `#v2-decision`
- [[concepts/claude-code-hooks]] — agent-boundary enforcement via PreToolUse hooks `#rescue` `#v2-decision`
- [[concepts/4d-methodology]] — Deconstruct/Diagnose/Develop/Deliver with ATOMIC/FEATURE/COMPLEX modes `#rescue` `#v2-decision`
- [[concepts/rfd-process]] — RFD methodology (states, frontmatter, body shape) `#rescue` `#v2-decision`

## Synthesis

- [[synthesis/positioning-rails-for-coding-assistants]] — v2 north-star positioning `#v2-decision`
- [[synthesis/auto-mode-v2-design]] — auto-mode in v2: built-in + hook points `#v2-decision`
- [[synthesis/orchestrator-as-plugin-loader]] — v1 orchestrator → v2 plugin loader mapping `#v2-decision`
- [[synthesis/v2-command-tree]] — proposed v2 CLI surface `#v2-decision`
- [[synthesis/v2-skill-migration-plan]] — how the 78 v1 skills become v2 plugins `#v2-decision`
- [[synthesis/v2-template-and-docs]] — shrink CLAUDE.md template + restructure mkdocs site `#v2-decision`
- [[synthesis/v2-rfds-to-write]] — eight inaugural v2 RFDs mapping wiki concepts to public docs `#v2-decision`
- [[synthesis/v2-rc-cycle-lessons]] — **post-launch.** rc.2 → rc.15 bug pattern, the Claude carve-out, and how the e2e suite closes the recurrence gap `#v2-decision`

## Queries

_Empty._

---

## Quick links

- [[CLAUDE]] — wiki conventions
- [[log]] — change log
