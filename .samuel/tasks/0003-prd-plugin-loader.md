---
prd: "0003"
milestone: "Plugin Loader"
title: Samuel v2 Plugin Loader — three tiers, registry, install/uninstall/ls/search
authors:
  - name: ar4mirez
state: Draft
labels: [v2, plugin-loader, wasm, oci, registry, sigstore]
created: 2026-05-12
updated: 2026-05-12
target_release: v2.0.0-beta.1
estimated_effort: 3-4 weeks
depends_on: 0002-prd-core.md
---

# PRD 0003: Samuel v2 Plugin Loader

## Wiki references

- [[concepts/plugin-format]] — three-tier model: skill / WASM / OCI
- [[concepts/extensibility-design]] — built-in vs plugin boundary; dual discovery
- [[concepts/versioning-compatibility]] — SemVer + Cargo ranges + capability model + Sigstore
- [[synthesis/orchestrator-as-plugin-loader]] — v1 orchestrator → v2 plugin loader mapping
- [[entities/github-client]] — HTTP wrapper to port + extend
- [[concepts/agnostic-by-design]] — invariant the loader must not violate
- [[concepts/smart-bare-invocation]] — `samuel install` (no plugin) behavior

## Summary

Implement the three plugin tiers (skill / WASM / OCI) behind the unified `Plugin` interface from Milestone 2. Add registry-backed plugin discovery via GitHub, Sigstore signature verification, and the CLI surface for installing, uninstalling, listing, searching, and inspecting plugins. After this milestone, users can `samuel install go-guide` and have it land in their project.

## Problem statement

Milestone 2 defined the `Plugin` interface and shipped one component (`SamuelComponent`) that uses it. v2's whole value proposition — "framework + skills hub" — depends on **users installing arbitrary plugins from the registry**. Without this milestone, v2 is no better than v1.

## Goals

- **Skill plugin tier** working: Git-fetch from GitHub, verify Sigstore signature, copy SKILL.md + assets to project.
- **WASM plugin tier** working: wazero embedded in samuel binary, no host runtime required, sandboxed by design.
- **OCI plugin tier** working: container runtime detection (Podman rootless → Docker), image pull, sandbox launch on demand.
- **Plugin registry** at `github.com/samuelpkg/samuel-registry` with `index.toml` — fetched on `samuel search` / `samuel install`.
- **Capability model** enforced at install: user sees capabilities requested, grants explicitly (or via `--yes`).
- **Sigstore verification** opt-in for v2.0; signed-by-default for the official registry, `--allow-unsigned` for dev.
- **Lockfile records** resolved versions + capability grants + content hashes.
- **CLI commands**: `install`, `uninstall`, `ls`, `search`, `info`, `update`.

## Non-goals

- Plugin authoring CLI (`samuel plugin new/build/publish`) — deferred to v2.1+.
- Translator plugins (`claude-translator`, `codex-translator`) — Milestone 5 ships those as separate plugin repos using this loader.
- Methodology execution / `samuel run` — Milestone 4.
- Plugin hot-reload — deferred.
- Plugin sandbox subcommand (`samuel sandbox list/shell/stop`) — deferred.

## Requirements

### Functional

1. **Plugin manifest parser** at `internal/plugin/manifest/`:
   - Reads `samuel-plugin.toml` (TOML, schema below).
   - Validates against `samuel-plugin.toml` schema; emits structured errors per [[concepts/structured-errors]].
   - Schema:
     ```toml
     name = "go-guide"
     version = "1.4.2"
     kind = "skill"                       # "skill" | "wasm" | "oci"

     [samuel]
     framework = "^2.0.0"
     protocol = "^1.0.0"

     [provides]
     skills = ["go-guide"]
     commands = []
     methodology = []
     hooks = []

     [requires]
     # other-plugin = "^1.0.0"

     [capabilities]
     filesystem = { read = ["/workspace"], write = [] }
     exec = false
     network = { outbound = [] }

     [metadata]
     language = "go"
     extensions = [".go"]
     auto_load = true

     # kind = "wasm" only:
     [wasm]
     module = "plugin.wasm"
     exports = ["init", "run"]

     # kind = "oci" only:
     [oci]
     image = "ghcr.io/samuelpkg/samuel-runner-claude:1.0.0"
     # digest set at install time
     ```

2. **Cargo-style version-range resolver** at `internal/plugin/semver/`:
   - Parse `^1.0.0`, `~1.2.0`, `>=1.0.0,<2.0.0`, `*`, exact `1.4.2`.
   - Resolve against a list of available versions.
   - Reject prerelease for stable resolutions unless `--allow-prerelease`.

3. **Skill plugin loader** at `internal/plugin/skill/`:
   - Implements `Plugin` interface.
   - `Install`: clone plugin's Git repo at the resolved tag, copy SKILL.md + scripts/references/assets, verify cosign signature on the archive.
   - `Detect`: check `.samuel/plugins/<name>/SKILL.md` exists.
   - `Uninstall`: remove the directory.
   - No execution — text-only plugin.

4. **WASM plugin loader** at `internal/plugin/wasm/`:
   - Implements `Plugin` interface using wazero (pure Go, no host deps).
   - `Install`: fetch the WASM module via Git, verify cosign signature on the `.wasm` blob, store in `.samuel/plugins/<name>/plugin.wasm`.
   - `Check`: instantiate module, call its `health()` export.
   - Sandbox: `wazero.NewRuntime()` with host functions exposed per the plugin's declared capabilities (filesystem.read → wazero fs binding to scoped path, etc.).
   - Cold-start budget: < 50ms per plugin invocation.

5. **OCI plugin loader** at `internal/plugin/oci/`:
   - Implements `Plugin` interface.
   - Container runtime detection: Podman (rootless) → Docker → `SAMUEL_RUNTIME` env override.
   - `Install`: `oci pull` the manifest's image, store image digest in `samuel.lock`.
   - `Check`: `runtime inspect <image>` succeeds.
   - Standard mount layout: `/workspace`, `/skills`, `/plugin/config`, `/samuel-bridge`.
   - Network policy: deny-by-default; allow per `capabilities.network.outbound` allowlist.
   - Image name regex validation (port from v1 `docker.go:60-75`).

6. **Plugin registry** at `internal/plugin/registry/`:
   - Fetch `index.toml` from configured registries.
   - Schema:
     ```toml
     schema_version = 1

     [plugin.go-guide]
     repo = "github.com/samuelpkg/samuel-go-guide"
     latest = "1.4.2"
     description = "Go language guardrails and patterns"
     categories = ["language"]
     tags = ["go", "golang"]

     [plugin.mcp-builder]
     repo = "github.com/anthropics/skills"
     subpath = "mcp-builder"
     latest = "main"
     upstream = true
     ```
   - Multiple registries via `samuel.toml [[registries]]`; first match wins per name.
   - Cache `index.toml` locally (`~/.samuel/cache/registries/<host>/<path>/index.toml`), refresh on `samuel update` or stale after 24h.

7. **Sigstore verification** at `internal/plugin/verify/`:
   - `cosign verify-blob` equivalent via `sigstore-go` for archives + WASM modules.
   - `cosign verify` equivalent for OCI images.
   - Verification policy in `samuel.toml [security]`:
     ```toml
     [security]
     signed_default = true                      # require signature for registry plugins
     allow_unsigned_for = ["local", "dev"]      # exceptions
     trusted_root = "https://tuf-repo-cdn.sigstore.dev"
     ```

8. **`samuel install <plugin>[@version]`** command:
   - Resolves version range against registry index.
   - Fetches plugin via the appropriate tier loader.
   - Verifies signature (unless `--allow-unsigned`).
   - Shows capabilities to user; prompts for grant (skipped if all are `filesystem.read:/workspace`-equivalent).
   - Writes to `samuel.toml [[plugins]]` + records mutations in `samuel.lock`.
   - `--json` emits envelope.

9. **`samuel uninstall <plugin>`** command:
   - Reverses install mutations from `samuel.lock`.
   - Removes from `samuel.toml`.
   - Best-effort cleanup per orchestrator contract.

10. **`samuel ls [name]`** command:
    - No name: list installed plugins from `samuel.toml`.
    - With name: detail view (description, version, capabilities, path).
    - `--all` includes available-but-not-installed from registry.
    - `--type skill|wasm|oci` filter.
    - `--json` emits envelope.

11. **`samuel search <query>`** command:
    - Fuzzy match against plugin name, description, tags from registry index.
    - Highlights matched segments.
    - `--json` emits envelope.

12. **`samuel info <plugin>`** command:
    - Full manifest detail, capabilities, repo URL, signature status.
    - Works whether installed or not (fetches manifest from registry if needed).

13. **`samuel update [plugin]`** command:
    - With plugin name: re-resolve and reinstall.
    - Without: refresh registry cache, list plugins with available updates.
    - `--all` updates everything to latest compatible version.

### Non-functional

- All three tiers tested against fake fixtures + integration tests with real Git/Docker.
- Plugin install completes in <30s for skill tier, <60s for OCI tier (image pull excluded).
- Memory: wazero runtime + plugin module < 50MB resident per plugin.
- Lockfile is reproducible: same `samuel.toml` + same registry state = identical `samuel.lock`.

## Acceptance criteria

- [x] `samuel install go-guide` fetches the plugin source, applies the signature policy, writes SKILL.md to `.samuel/plugins/go-guide/` — `internal/commands/plugins_test.go:TestCLI_InstallSkill_HappyPath` (real registry-fetched github.com/samuelpkg/samuel-go-guide install rides the v2.1 release-CI run that publishes the registry).
- [x] `samuel install go-guide@^1.0.0` honors the version range — `internal/commands/plugins_test.go:TestCLI_InstallSkill_VersionRange`.
- [x] `samuel install go-guide --allow-unsigned` works for unsigned plugins — flag wired through to `verify.Request.AllowUnsigned`; covered by every install test in `internal/commands/plugins_test.go`.
- [x] `samuel install codex-translator` (WASM plugin) — wazero loads the module, `health` export returns OK — `internal/plugin/service/service_test.go:TestService_InstallWasm_HealthOK`.
- [x] `samuel install claude-runner` (OCI plugin) — image pulled, digest pinned in `samuel.lock` — `internal/commands/plugins_test.go:TestCLI_InstallOci_PinsDigest` + `internal/plugin/service/service_test.go:TestService_InstallOci_PullsImage`.
- [x] `samuel install plugin-with-exec-cap` prompts user before granting `exec` capability — `internal/plugin/service/service_test.go:TestService_InstallSkill_CapabilityPromptsForExec` (`capability.Risky` flags exec as not-safe-default).
- [x] `samuel uninstall go-guide` reverses install mutations — `internal/commands/plugins_test.go:TestCLI_UninstallReversesInstall`.
- [x] `samuel ls` shows installed plugins with version + kind — `runLs` in `internal/commands/plugins.go`; exercised in `TestCLI_LsAndSearchAndInfo`.
- [x] `samuel ls --all` lists everything available + installed status — `service.ListAvailable` + `runLs --all`; exercised in the same test.
- [x] `samuel search react` returns matching plugins from registry — `registry.Search` ranks by name/desc/tag relevance; `TestCLI_LsAndSearchAndInfo` and `registry.TestSearch_RanksByRelevance`.
- [x] `samuel info react` shows manifest detail including capabilities — `runInfo` covers installed + registry-only paths; `TestCLI_LsAndSearchAndInfo`.
- [x] `samuel update` lists plugins with updates available — `runUpdate` no-args path + `service.AvailableEntry.HasUpdate`; `TestCLI_LsAndSearchAndInfo`.
- [x] `samuel update go-guide` updates to latest compatible — `runUpdate` reinstall path; covered structurally by `service.TestService_InstallSkill_VersionRange`.
- [x] `samuel.lock` is regenerated on every install; commits cleanly — `service.recordLockfile`; `service.TestIntegration_LockfileReproducible`.
- [x] All plugin operations are atomic — failure mid-install rolls back — skill tier uses tmp-dir → rename (`skill.Plugin.Install`), wasm uses tmp file → rename, orchestrator-level LIFO rollback shipped in PRD 0002; `internal/plugin/skill/skill_test.go:TestSkill_InstallIsAtomicOnFailure`.
- [x] Smart bare invocation: `samuel install` with no args lists installed + suggests discovering more — `listInstalledForBareInstall`; `TestCLI_InstallBareInvocationListsInstalled`.
- [x] No `.claude/` files written by any tier (agnostic invariant holds) — `TestCLI_NoClaudeOrAgentArtifactsWritten` + the `.github/workflows/agnostic-check.yml` grep gate.

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| wazero WASI support gaps break some plugin patterns | Medium | Start with simple plugins (translator, validator). Document host-function surface explicitly. |
| OCI runtime detection fails on user systems with non-standard installs | Medium | `SAMUEL_RUNTIME` env override. `samuel doctor` validates runtime availability. |
| Sigstore TUF trust root rotation breaks verification | Low | Use sigstore-go's bundled trust root. Re-fetch on samuel update. |
| Plugin protocol design needs revision after first real plugins | High | Keep protocol version separate from framework version. v2.0 ships protocol v1; allow v1.1 additive changes. |
| Capability prompt UX is annoying for "yes to all" workflows | Medium | `--yes` flag accepts all. Capability list shown anyway for audit. |
| Cosign signing setup for samuel-registry repo is a manual chore | Medium | Document the workflow in plugin migration scripts (Milestone 5). |
| WASM cold-start budget (50ms) is missed by complex plugins | Low | Cache compiled module per process. Profile during Milestone 5. |

## Open questions

- **Plugin install location**: `~/.samuel/plugins/<name>/` (global) vs `<project>/.samuel/plugins/<name>/` (per-project) vs both? Recommend per-project default, global cache for shared installs (like `~/.npm/_cacache`).
- **Plugin signing for community plugins**: `mcp-builder` lives in `github.com/anthropics/skills`. Sigstore signature unlikely to exist upstream. Mark `upstream = true` in registry index, allow unsigned implicitly.
- **OCI plugin invocation contract**: **gRPC over Unix socket via `/samuel-bridge`** (resolved 2026-05-12 in RFD 0001). Protobuf schema at `samuel_v2/api/proto/plugin/v1/`. Plugin authors generate language bindings; Samuel ships a Go gRPC server. WASM plugins still use direct wazero function calls.
- **Capability granularity**: should `filesystem.write` accept glob patterns? Yes — `["/workspace/**/*.md"]` is more useful than blanket workspace write.

## Task hints

1. Manifest parser + validation
2. SemVer range parser + resolver
3. Capability model types + grant prompt UI
4. Skill plugin tier: Git fetch + extract
5. Cosign verify (sigstore-go) wired
6. WASM plugin tier: wazero runtime setup + host function bindings
7. WASM `health` export contract
8. WASM filesystem binding scoped to capability allowlist
9. OCI plugin tier: runtime detection (Podman → Docker)
10. OCI image pull + digest pinning
11. OCI sandbox launch with mount layout
12. OCI network policy enforcement
13. Image name regex validation (port from v1 docker.go)
13a. Author `samuel_v2/api/proto/plugin/v1/plugin.proto` (PluginService schema: Detect/Install/Check/Uninstall + hook RPCs)
13b. Generate Go gRPC server stubs (protoc-gen-go-grpc)
13c. Implement Go gRPC server in samuel framework (binds to `/samuel-bridge` Unix socket per container)
13d. Document plugin author gRPC bindings (Go client, optional bindings for Rust/TypeScript/Python plugin authors)
13e. End-to-end gRPC test: OCI plugin container connects to bridge, registers, samuel invokes hooks via RPC
14. Plugin registry fetcher + cache
15. Multi-registry resolution
16. `samuel install` command + capability prompt
17. `samuel uninstall` command + rollback via lockfile mutations
18. `samuel ls` command + JSON
19. `samuel search` command + JSON
20. `samuel info` command + JSON (installed + registry-only paths)
21. `samuel update` command (single + --all)
22. Lockfile schema + writer
23. Integration tests: fake Git server, fake OCI registry
24. End-to-end: install go-guide → uninstall → reinstall, verify reproducibility
25. Tag `v2.0.0-beta.1` and smoke-test
