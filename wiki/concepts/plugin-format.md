---
title: Plugin format & sandbox (v2)
type: concept
created: 2026-05-12
updated: 2026-05-12
sources: []
tags: [v2, v2-decision, open]
---

# Plugin format & sandbox

Three-tier plugin model. Skills as text, WASM for sandboxed exec without host deps, OCI for full-power isolation when needed.

## The "embed a runtime" question

You asked: can Samuel embed a container runtime so users have zero host dependencies?

**Honest answer: not a full Linux container runtime.** Containers depend on Linux kernel features (cgroups, namespaces, seccomp). On macOS or Windows, the kernel isn't Linux — Docker Desktop bundles a Linux VM under the hood. Samuel would have to ship that VM (hundreds of MB) and still need elevated privileges to start it. Same for Podman Machine, lima, colima.

**What Samuel can embed: WebAssembly (WASM).** Pure-Go runtimes (wazero, BSD-3, used by Tetrate, Tetragon, Envoy proxy filters) link directly into Samuel's binary. Zero host deps. Sandbox-by-default. Cross-platform identical. The cost: plugins must be compiled to WASM, which constrains what they can do (filesystem and network go through host bridges, async is awkward, no fork/exec).

So: three tiers, picked per plugin's actual needs.

## The three tiers #v2-decision

| Tier | Sandbox | Host requirement | Use for |
|---|---|---|---|
| **Skills** | None (no exec) | None | Text knowledge — language guides, framework guides, methodology docs, templates |
| **WASM plugins** | wazero (embedded in Samuel) | None | Most executable plugins — transformers, validators, doc generators, custom commands, hooks |
| **OCI plugins** | Host container runtime (Docker/Podman) | Docker or Podman installed | Heavy plugins — running coding assistants (Claude Code, Codex), language-specific tools, anything needing real subprocess + Linux userland |

### Why this is the right split

- **80% of plugins fit WASM.** Reading SKILL.md, generating an AGENTS.md, validating a manifest, rendering a template, translating skills between formats — none of that needs Linux.
- **WASM keeps "samuel works out of the box" true** for the package-manager half of Samuel. No Docker required just to install plugins and use skills.
- **OCI stays in scope** for the task-execution half, where you actually run an external coding-assistant CLI. That use case fundamentally needs full Linux + the assistant's runtime (Node for Claude Code, Python for many others).

### How users feel it

```
samuel install some-plugin
# WASM plugin → installs instantly, no warnings

samuel install some-other-plugin
# OCI plugin → "This plugin requires Docker or Podman. Install one? [y/N]"

samuel run claude
# Detects coding-assistant launch → needs OCI runtime → checks Docker/Podman
```

Skill-only and WASM-only workflows never see a container prompt.

## Manifest format #v2-decision (TOML default)

```toml
# samuel-plugin.toml
name = "go-guide"
version = "1.4.2"
kind = "skill"   # "skill" | "wasm" | "oci"

[samuel]
framework = "^2.0.0"
protocol = "^1.0.0"

[provides]
skills = ["go-guide"]
commands = []
methodology = []

[requires]
# other-plugin = "^1.0.0"

[capabilities]
filesystem = { read = ["/workspace"] }
```

For WASM plugins:

```toml
kind = "wasm"
[wasm]
module = "plugin.wasm"
exports = ["init", "run"]
```

For OCI plugins:

```toml
kind = "oci"
[oci]
image = "ghcr.io/ar4mirez/samuel-runner-claude:1.0.0"
digest = "sha256:..."   # populated at install, locked in samuel.lock
```

YAML supported (`samuel-plugin.yaml`) for users who prefer it. TOML default everywhere Samuel-specific. SKILL.md frontmatter stays YAML per the [[concepts/agent-skills-standard]].

## Sandbox layer details

### WASM plugins

- wazero runtime initialized once per Samuel process.
- Each plugin invocation = fresh module instance (cheap with module caching).
- Host functions exposed: filesystem (gated by capabilities), config read, log emit, callback into Samuel API.
- No network from inside the module — bridge through host functions if `network.outbound` capability granted.

### OCI plugins / coding-assistant execution

- Samuel detects container runtime (Docker, Podman, nerdctl).
- Standard mount layout:
  - `/workspace` (read-write or read-only by capability)
  - `/skills` (read-only)
  - `/plugin/config` (read-only)
  - `/samuel-bridge` (Unix socket back to host Samuel)
- Image pulled from any OCI registry; GHCR recommended for the same identity story as the plugin registry.

## Signing & trust #v2-decision

**Sigstore opt-in default.**

- WASM modules: sign with cosign, store signature alongside in the plugin Git repo (sigstore-go can verify in-process).
- OCI images: cosign signature on the registry, verified before launch.
- Default registry config: signed-only. User can `--allow-unsigned` for development or registries that haven't adopted signing yet.

Provenance attestation (SLSA Level 2+) is encouraged but not required for v2.0. Document the path; ship verification later.

## Resolved follow-ups #v2-decision

- **Blessed WASM toolchain**: **TinyGo** first. Ship a `samuel plugin new --tinygo` scaffold + base imports. Rust supported (any WASI-compatible toolchain works); AssemblyScript documented but not blessed.
- **Container runtime detect order**: Podman (rootless preferred) → Docker → other OCI-compatible runtimes. `SAMUEL_RUNTIME` env var overrides detection.
- **OCI plugin invocation protocol** (resolved 2026-05-12, RFD 0001 Committed): **gRPC over Unix socket via `/samuel-bridge`**. Protobuf schema at `samuel/api/proto/plugin/v1/`. Higher overhead than stdio JSON but enables streaming, bidirectional capability calls, and strong typing. WASM plugins continue to use direct wazero function calls (no protocol needed for in-process invocation).

## Open

- Performance: WASM cold-start budget. Aim < 50ms per plugin invocation.
- Network policy granularity for OCI bridge — host-based allowlist, regex, deny-by-default with explicit consents.
