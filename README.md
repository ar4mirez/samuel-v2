# Samuel

Rails for AI coding assistants. A small Go CLI that ships a framework, a plugin loader, and an opinionated methodology — everything tool-specific lives in plugins.

[![CI](https://github.com/samuelpkg/samuel/actions/workflows/ci.yml/badge.svg)](https://github.com/samuelpkg/samuel/actions/workflows/ci.yml)
[![Release](https://github.com/samuelpkg/samuel/actions/workflows/release.yml/badge.svg)](https://github.com/samuelpkg/samuel/actions/workflows/release.yml)
[![Docs](https://github.com/samuelpkg/samuel/actions/workflows/docs.yml/badge.svg)](https://samuelpkg.github.io/samuel/)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

## What it is

- **Agent-agnostic with a Claude carve-out.** `AGENTS.md` is the canonical project context. `.cursor/rules/`, `.codex/*`, and future tool-specific surfaces are produced by translator plugins — the framework knows about none of them. The lone exception is the AGENTS.md → CLAUDE.md mirror, which ships in core because Claude Code is the only major coding assistant that doesn't read AGENTS.md natively.
- **Plugin-driven.** Three tiers: **skill** (text + scripts), **WASM** (sandboxed via wazero), **OCI** (containers for heavy tools). Plugins are intended to be signed by Sigstore keyless OIDC. **v2.0 caveat:** the default verifier is a policy-only stub — `identity_patterns` and `allow_unsigned_for` are enforced, but no Sigstore math runs. `samuel doctor` prints a one-line advisory until v2.1 wires the sigstore-go backend.
- **Methodology built in.** The 4D loop (Deconstruct / Diagnose / Develop / Deliver) with Ralph-Wiggum-style iteration cap as the default. Methodology plugins add hooks; the framework runs the loop.
- **TOON-encoded runtime.** State files (`.samuel/run/*.toon`) are token-efficient for LLM context. Markdown stays for prose-heavy logs.
- **CLI-mutation pattern.** The agent never edits state directly — it runs `samuel run done <id>` or `samuel run skip <id>`. The CLI owns the schema; the agent owns the decisions.

Full design rationale: read [RFDs 0001–0008](https://samuelpkg.github.io/samuel/rfd/).

## Install

**Homebrew (macOS/Linux):**

```bash
brew update
brew install ar4mirez/tap/samuel
```

**curl install script:**

```bash
curl -sSL https://raw.githubusercontent.com/samuelpkg/samuel/main/install.sh | sh
```

**go install:**

```bash
go install github.com/samuelpkg/samuel/cmd/samuel@latest
```

Verify:

```bash
samuel version
samuel doctor
```

> Every release artifact is signed by Sigstore keyless OIDC. See the cosign verification snippet in any release's notes.

## 60-second tour

```bash
samuel init my-project
cd my-project

# AGENTS.md is now in the working tree — that's your canonical context.
# Add a plugin:
samuel install go-guide

# (Optional) keep CLAUDE.md generated for tools that read it:
samuel install samuel-claude-translator

# Start the autonomous loop:
samuel run start
```

The autonomous loop is iteration-capped (Ralph Wiggum methodology) and emits hooks at every boundary. Plugins attach to the hooks; the framework drives the loop. Full walkthrough in [Quick Start](https://samuelpkg.github.io/samuel/getting-started/quick-start/).

## Documentation

- **Site**: [samuelpkg.github.io/samuel](https://samuelpkg.github.io/samuel/)
- **Getting started**: [Installation](https://samuelpkg.github.io/samuel/getting-started/installation/), [Quick start](https://samuelpkg.github.io/samuel/getting-started/quick-start/), [Your first task](https://samuelpkg.github.io/samuel/getting-started/first-task/), [Migrating from v1](https://samuelpkg.github.io/samuel/getting-started/migration-v1/)
- **Concepts**: [overview](https://samuelpkg.github.io/samuel/core/overview/), [plugin format](https://samuelpkg.github.io/samuel/concepts/plugin-format/), [AGENTS.md primary](https://samuelpkg.github.io/samuel/concepts/agents-md-primary/), [methodology hooks](https://samuelpkg.github.io/samuel/concepts/methodology-hooks/)
- **Plugin authoring**: [manifest](https://samuelpkg.github.io/samuel/plugin-authors/manifest/), [hooks](https://samuelpkg.github.io/samuel/plugin-authors/hooks/), [capabilities](https://samuelpkg.github.io/samuel/plugin-authors/capabilities/), [TinyGo + WASM](https://samuelpkg.github.io/samuel/plugin-authors/tinygo-wasm/), [OCI + gRPC](https://samuelpkg.github.io/samuel/plugin-authors/oci-grpc/), [signing](https://samuelpkg.github.io/samuel/plugin-authors/signing/)
- **Reference**: [CLI](https://samuelpkg.github.io/samuel/reference/cli/), [FAQ](https://samuelpkg.github.io/samuel/reference/faq/), [cross-tool generation](https://samuelpkg.github.io/samuel/reference/cross-tool/)
- **RFDs (design record)**: [index](https://samuelpkg.github.io/samuel/rfd/)

## For v1 users

v2 is a clean break. The binary name is the same; installing v2 overwrites v1. The v1 source lives at the [`v1-final`](https://github.com/samuelpkg/samuel/tree/v1-final) tag.

If you used `CLAUDE.md` directly, install the [Claude translator plugin](https://github.com/samuelpkg/samuel-claude-translator) — it mirrors `AGENTS.md → CLAUDE.md` on every `samuel sync`. If you used `gstack` or `gbrain`, see [RFD 0008](https://samuelpkg.github.io/samuel/rfd/0008/) for the rationale and migration path.

Full notice: [Migrating from v1](https://samuelpkg.github.io/samuel/getting-started/migration-v1/).

## Repo layout

```text
samuel_v2/
├── cmd/samuel/             # CLI entry point
├── internal/
│   ├── commands/           # cobra commands (init, install, run, sync, doctor, plugin, version)
│   ├── methodology/        # built-in methodologies (ralph)
│   ├── orchestrator/       # component lifecycle + rollback
│   ├── plugin/             # three tiers + manifest + capability + verify + registry
│   ├── sync/               # AGENTS.md generator
│   ├── ui/                 # Charm UI (lipgloss + huh + bubbles)
│   └── ...
├── template/AGENTS.md.tmpl # canonical template (≤150 lines, CI-enforced)
├── docs/                   # mkdocs site (deployed to samuelpkg.github.io/samuel/)
├── scripts/                # release-checklist, docs/RFD generators, v1 deprecation
├── .goreleaser.yaml        # signed builds + brew tap + cosign bundle
└── rfd-index.toml          # RFD source of truth
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Bug? [Open one](https://github.com/samuelpkg/samuel/issues/new?template=bug_report.yml). Idea? [Discussions](https://github.com/samuelpkg/samuel/discussions). Vulnerability? [Private advisory](https://github.com/samuelpkg/samuel/security/advisories/new) — never a public issue.

## Changelog + RFDs

- [CHANGELOG.md](CHANGELOG.md) — per-version release notes (Keep-a-Changelog format).
- [docs/rfd/](docs/rfd/) — design record (RFDs 0001–0008 inaugural).

## License

MIT — see [LICENSE](LICENSE).
