# Wiki conventions

This file tells Claude how to maintain this specific wiki. The `llm-wiki` skill provides the generic rules. This file captures domain conventions for this wiki.

## Domain

This wiki tracks the design and rebuild of **Samuel** — an opinionated AI coding framework for professional software teams. It provides guardrails, language guides, and workflows that help AI coding assistants produce consistent, high-quality code.

Two codebases coexist in `/Users/ar4mirez/Documents/Claude/Projects/Samuel/`:

- `samuel_v1/` — the original shipped version (Go CLI, mkdocs site, ~30+ language skills, `.claude/` hooks, RFD index). Reference material only — extract patterns worth keeping.
- `samuel/` — **shipped at v2.0.0-rc.15** as of 2026-05-13. The rebuild lives here. Goals: simplify, make extensible — both achieved. Manual-test fixture lives at `samuel/examples/tetris/`; hermetic e2e suite at `samuel/e2e/hermetic/`.

The wiki sits between these two. It is the place where v1 patterns get studied, v2 design decisions get recorded, and the gap between them gets bridged. **Post-launch**, it also ingests lessons from the v2 release-candidate cycle — see [[synthesis/v2-rc-cycle-lessons]] for the rc.2 → rc.15 review.

## Page templates

- **Sources** (`sources/`): one page per ingested artifact — v1 file, doc, transcript, external article, design note, conversation snippet. Frontmatter, brief context, key claims with file/line citations, links to entities and concepts mentioned.
- **Entities** (`entities/`): named things — components (`cli`, `auto-mode`, `skill-loader`), files (`AGENTS.md`, `samuel.yaml`), tools (`mkdocs`, `goreleaser`), people, external products.
- **Concepts** (`concepts/`): ideas and frameworks — `skills-architecture`, `rfd-process`, `language-guides`, `guardrails`, `auto-mode`, `extensibility`.
- **Synthesis** (`synthesis/`): cross-cutting analyses — `v1-vs-v2-comparison`, `what-to-rescue`, `simplification-thesis`, `extensibility-design`.
- **Queries** (`queries/`): filed-back answers to questions the user asked.

## Conventions specific to this wiki

- **v1 references**: when citing v1 code, link the source page and include the repo-relative path: `[[sources/2026-05-12-v1-project-meta]] (samuel_v1/README.md)`.
- **No raw/ copies for v1 code**: `samuel_v1/` is committed in the project (has its own `.git`). The source is permanently recoverable. Cite by `samuel_v1/...` path instead of copying to `raw/`. Reserve `raw/` for fetched URLs and external uploads.
- **v2 decisions**: tag `#v2-decision` on any page that captures a binding choice for the rebuild. Include rationale and what v1 pattern it replaces or rescues.
- **Rescue tag**: tag `#rescue` on entity/concept pages that represent v1 functionality the user explicitly wants to carry forward.
- **Drop tag**: tag `#drop` for v1 functionality intentionally being removed.
- **Open**: tag `#open` for unresolved design questions. The lint pass surfaces these.
- Use kebab-case slugs throughout. Source pages prefix with `YYYY-MM-DD-`.

## Resolved scope decisions (v2) #v2-decision

- **Runtime**: Go (same as v1).
- **Release strategy**: clean break. v2 deprecates v1 immediately on release. No migration tool, no parallel support.
- **Positioning**: **"Rails for coding assistants."** Package manager for AI coding tools + task execution layer with baked-in methodology. Thin client, opinionated conventions.
- **Built-in scope (THIN)**: universal coding-assistant primitives only — AGENTS.md, SKILL.md, design.md, anything cross-tool standard (Claude Code, Codex, etc.). Plugin loader. Methodology hooks.
- **Plugin scope**: language guides, framework guides, workflow skills, methodology packs, integrations. Anything tool- or domain-specific.
- **Plugin discovery**: **dual** — local filesystem + GitHub-backed registry. No fancy registry server. Plugin = a Git repo.
- **Plugin format**: **three tiers**
  - **Skills** (text knowledge) — Git repos / tar archives. No exec. No host deps.
  - **WASM plugins** — sandboxed by `wazero` embedded in the Samuel binary. No host deps. Default for most executable plugins.
  - **OCI plugins** — host Docker/Podman runtime. Used when full Linux userland needed (running coding assistants, language-specific tools).
- **Coding-assistant execution**: runs in an OCI sandbox container. Claude Code first. Docker/Podman required only for this case + OCI plugins.
- **Versioning**: **SemVer 2.0.0**. Cargo-style ranges. Three independent version axes (framework / plugin protocol / plugin). Capability-permission model declared in manifest, enforced at sandbox boundary.
- **Signing**: **Sigstore/cosign signed-by-default** for the official registry, `--allow-unsigned` for dev. **v2.0 ships a policy-only `StubVerifier`** — `identity_patterns` + `allow_unsigned_for` are enforced but the Sigstore math itself rides v2.1 (sigstore-go swap). `samuel doctor` surfaces this inline as an advisory. The wire format and lockfile schema are stable across the v2.0 → v2.1 transition.
- **Config format**: **TOML default**, YAML supported. SKILL.md frontmatter stays YAML per Agent Skills standard.
- **Blessed WASM toolchain**: **TinyGo** first (Go-native, matches plugin author base). Rust and AssemblyScript secondary.
- **Container runtime detect order**: Podman (rootless) → Docker → others. `SAMUEL_RUNTIME` env var overrides.
- **gstack and gbrain dropped from v2.** Neither survives the rebuild. May reintroduce as plugins later, or extract reusable pieces as skills. v1 product opinions, not framework essentials.
- **CLI verb**: keep `init` (not `new`). Works for both new and existing projects. `new` reserved for any future create-only verb.
- **AGENTS.md primary, with a scoped Claude carve-out.** v2 writes AGENTS.md by default. The `AGENTS.md → CLAUDE.md` mirror ships as a **built-in translator** (`internal/translator/claude/`) — shipped in **v2.0.0-rc.4** after manual-test data showed every other major coding assistant (Codex, Aider, Cursor, Gemini, Cline) reads AGENTS.md natively but Claude Code does not, making the mirror friction-without-payoff to require as a plugin install. Cursor rules, Codex specifics, Continue rules, and every richer tool-specific surface stay as translator plugins. See [[concepts/agents-md-primary]] and [[synthesis/v2-rc-cycle-lessons]].
- **Sync is hook + command.** Same code path, two trigger surfaces. Runs automatically at lifecycle points + manually via `samuel sync`.
- **No `samuel.dev` domain yet.** v2 stays on `ar4mirez.github.io/samuel/` for now. Rewrite v2 error DocsURLs to github.io URLs (no dead links). Register domain later if/when it makes sense.
- **TOON for `.samuel/run/` structured files** (`prd.toon`, `project-snapshot.toon`, `task-context.toon`). User-observed JSON fragility outweighs the theoretical AI-emit-JSON advantage. Line-oriented rows tolerate malformations better. Markdown stays for prose-heavy progress logs.
- **Agent stops writing prd directly.** Uses CLI subcommands (`samuel run done|skip|reset|enqueue`). Samuel CLI is the only mutator of runtime state. Encoding becomes a Samuel-internal concern.

## Ingest plan

Bottom-up. Lowest-dependency packages first, then layers built on top, then docs/meta last.

1. **Skill model** — `internal/skills/embed.go`, `internal/skills/README.md`, `internal/core/skill.go`, `internal/core/registry.go`. Defines what a "skill" is in v1.
2. **Config + sync** — `internal/core/config.go`, `sync.go`, `downloader.go`, `extractor.go`, `docker.go`; `samuel.yaml`.
3. **Auto-mode core** — `internal/core/auto*.go` (~12 files).
4. **Orchestrator** — `internal/orchestrator/*` (gbrain, gstack, samuel components, locking).
5. **GitHub client** — `internal/github/`.
6. **UI** — `internal/ui/` (json, output, prompts, spinner).
7. **Commands layer** — `internal/commands/*` (~30 files, group by family).
8. **CLI entry** — `cmd/samuel/main.go`, `cmd/CLAUDE.md`.
9. **`.claude/` runtime** — `auto/`, `hooks/`, `settings.json`.
10. **Skill content survey** — `internal/skills/content/*` and `.claude/skills/*` (note duplication — same content, two paths).
11. **Templates** — `template/`.
12. **Docs site** — `docs/*`, `mkdocs.yml`.
13. **RFDs** — `docs/rfd/0001-0004.md`, `rfd-index.yaml`.
14. **Build/release** — `Makefile`, `install.sh`, `.goreleaser.yaml`, `.github/workflows/*`.
15. **Project meta** — `README.md`, `AGENTS.md`, `CLAUDE.md`, `CHANGELOG.md`.

## Open questions

- Methodology graduation: confirmed pattern is **default built-in + optional plugin enhancement** (#v2-decision 2026-05-12). Each v1 workflow needs a per-workflow call: is the built-in version full-featured, or just a "minimal Samuel Way" with the heavyweight bits available as plugins? Resolve per-workflow in passes 3, 6, 10.
- **Auto-mode rename**: `samuel auto` → `samuel run [methodology]` with default methodology in `samuel.toml`. Niche name `rw` (Ralph Wiggum) supported as a methodology alias. #v2-decision 2026-05-12.
- **Discovery-only mode**: `samuel run --discover-only` ships as built-in. #v2-decision 2026-05-12.
- **Prompt template variables**: spec filed in [[concepts/prompt-template-variables]]. #v2-decision 2026-05-12.
- Identity/auth for private plugin registries on GitHub — GH token via env, or full GH App?
- WASM cold-start budget target — aim < 50ms per invocation.
- Network policy granularity for OCI bridge — host-based allowlist, regex, or strict deny-by-default with explicit per-call consents?

---
Created: 2026-05-12
