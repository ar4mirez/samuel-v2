---
title: Samuel v2 (rebuild target)
type: entity
created: 2026-05-12
updated: 2026-05-12
sources: []
tags: [anchor, v2, v2-decision]
---

# Samuel v2

The rebuild. Lives at `samuel/` (currently empty). Clean break — v1 will be deprecated on release as if it never existed.

## Positioning

**Rails for coding assistants.** See [[synthesis/positioning-rails-for-coding-assistants]].

Samuel is a package manager for AI coding tools + a task execution layer with baked-in methodology. Thin client, opinionated conventions. Not a coding assistant itself.

## Resolved decisions #v2-decision

- **Runtime**: Go.
- **Release**: clean break. No migration, no parallel, no compat shims.
- **Built-in scope (THIN)**: only cross-tool primitives — AGENTS.md, SKILL.md, design.md, plugin loader, methodology hooks. See [[concepts/extensibility-design]].
- **Plugin discovery**: dual — local filesystem + GitHub-backed registry (a repo with an index file). No central server.
- **Plugin format**: **three tiers** — skills (text), WASM (embedded wazero, no host deps), OCI (host Docker/Podman for heavy/assistant exec). See [[concepts/plugin-format]].
- **Coding-assistant execution**: runs inside an OCI sandbox container. Claude Code first; Codex next.
- **Versioning**: SemVer 2.0.0. Cargo-style ranges. Three version axes (framework / protocol / plugin). Capability model. See [[concepts/versioning-compatibility]].
- **Signing**: Sigstore/cosign signed-by-default for the official registry.
- **Config format**: TOML default. YAML supported. SKILL.md frontmatter stays YAML for cross-tool compat.

## Open design questions

- Container-runtime detect order (Podman vs Docker), `SAMUEL_RUNTIME` env override.
- WASM toolchain to bless first (TinyGo, Rust, AssemblyScript).
- Methodology graduation: which v1 workflows are core hooks vs plugin content (resolves in pass 6 + 10).
- Auth for private GitHub-backed registries (GH token vs GH App).
- WASM cold-start budget; OCI bridge network policy granularity.

## Anchors

- [[entities/samuel-v1]] — what's being replaced
- [[synthesis/positioning-rails-for-coding-assistants]] — north star
- [[concepts/extensibility-design]] — built-in vs plugin
- [[concepts/plugin-format]] — transport + sandbox
- [[concepts/versioning-compatibility]] — SemVer + compatibility contract
