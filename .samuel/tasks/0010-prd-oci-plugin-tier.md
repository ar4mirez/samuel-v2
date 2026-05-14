---
prd: "0010"
milestone: "OCI plugin tier + network policy"
title: Samuel v2.3 — OCI plugin tier, network policy, agent containerization
authors:
  - name: ar4mirez
state: Draft
labels: [v2, v2.3, plugins, oci, podman, docker, sandbox, network-policy]
created: 2026-05-13
updated: 2026-05-13
target_release: v2.3.0
estimated_effort: 3-4 weeks
depends_on: 0009-prd-wasm-plugin-tier.md
---

# PRD 0010: OCI Plugin Tier + Network Policy

## Wiki references

- [[concepts/plugin-format]] — three-tier plugin model; OCI tier scope
- [[CLAUDE]] — "Container runtime detect order: Podman (rootless) → Docker → others. SAMUEL_RUNTIME env var overrides."
- [[CLAUDE]] — open question: "Network policy granularity for OCI bridge — host-based allowlist, regex, or strict deny-by-default with explicit per-call consents?"
- [[CLAUDE]] — "Coding-assistant execution: runs in an OCI sandbox container. Claude Code first."
- [[sources/2026-05-12-v1-config-sync]] — v1's `docker.go` patterns and the multi-agent sandbox layer to draw from
- [[entities/docker-sandbox]] — v1's multi-agent sandbox layer (`#rescue`)

## Summary

Complete the OCI plugin tier and the OCI-backed agent sandbox. The `internal/plugin/oci/` package was scaffolded in v2.0 (launcher skeleton, bridge, runtime stub) but: (a) it doesn't resolve the open question on network policy granularity, (b) it has no coding-assistant execution path wired to `samuel run --sandbox=oci`, (c) no reference OCI plugin has shipped. This PRD lands all three, codifies the deny-by-default network policy with per-call consent prompts, and ships v2.3.0.

After v2.3, plugin authors can pick the right tier for their plugin: skill (knowledge), WASM (sandboxed execution, deterministic, fast), or OCI (full Linux userland, language flexibility, host runtime dependency). The agent itself runs in an OCI container by default in CI/production, removing the host-environment dependency that broke rc.16 (HOME/PATH passthrough).

## Problem statement

v2.0 ships with an OCI scaffold but two unresolved decisions and one absent capability:

1. **Network policy granularity is undecided.** The wiki open question lists three options: host-based allowlist, regex, or deny-by-default with explicit per-call consents. The decision has been blocked on having a real OCI use case. With agent containerization needed (rc.16 surfaced HOME/PATH passthrough complexity), that use case is now real.

2. **Container runtime detection is not implemented.** The wiki spec says "Podman (rootless) → Docker → others, SAMUEL_RUNTIME override." `internal/plugin/oci/runtime.go` has the detection stub but never resolves to a working runtime in tests or production code.

3. **Agent containerization is half-wired.** `internal/sandbox/sandbox.go:runHost` is the only mode currently used; `runOCI` exists but is incomplete. rc.16's HOME/PATH-stripping bug was a symptom: the right answer is to run agents in containers with their environment fully owned by Samuel, not to keep extending the host-exec env passthrough list.

4. **No reference OCI plugin.** Same gap as PRD 0009's WASM tier — plugin authors have nothing to copy.

This PRD lands all four. After v2.3, OCI is a first-class tier with documented policies, a working agent-container path, and at least one shipped reference plugin.

## Goals

- **Network policy: deny-by-default with explicit allowlist** (resolution of the open question, see Decision section).
- **Container runtime detection** ordered Podman-rootless → Podman-root → Docker → fail; `SAMUEL_RUNTIME` env var overrides.
- **`samuel run --sandbox=oci`** runs the agent in an OCI container with samuel-managed env. Default for v2.3+ in CI / production paths. Host mode (`--sandbox=host`) preserved for development.
- **Capability enforcement at OCI boundary**: filesystem volumes mapped per manifest, env injected explicitly, network governed by the deny-by-default policy.
- **Reference plugin**: `samuel-claude-code-oci` — packaging of Claude Code as an OCI plugin (Anthropic image + samuel-plugin.toml). Validates the full "agent as OCI plugin" pattern.
- **`samuel new plugin --kind=oci`** scaffolding.
- **Plugin-authoring documentation** at `docs/plugin-authors/oci.md`.
- **Live + hermetic e2e coverage** for the OCI tier (where the runtime is available — tier auto-skips when no container runtime is detected).
- **RFD 0011** — OCI plugin tier and network policy.

## Non-goals

- No bundled container runtime. Samuel does not ship Docker or Podman; users install one. `samuel doctor` reports the missing-runtime case clearly.
- No Kubernetes integration. OCI plugins run via local Podman/Docker; cluster execution is a future PRD if a real use case appears.
- No Windows container support. Linux containers only. Windows users run via WSL2 or skip OCI tier.
- No image building from inside Samuel. Plugin authors build with `docker build` / `podman build`; Samuel only loads and runs.
- No multi-stage network policy (e.g. "allow github.com only during install, deny during run"). v2.3 policy is single-phase.
- No bridge between OCI plugins and WASM plugins for shared state. Each plugin invocation is isolated.

## Decision: Network policy

After review of the three options listed in [[CLAUDE]]'s open question, the choice is **strict deny-by-default with explicit host allowlist + per-call consent prompts for unallowlisted hosts**.

Rationale:

- **Host-based allowlist (only)** is too permissive: a manifest can declare `github.com` and then exfiltrate via DNS lookups, redirects, or sibling hosts.
- **Regex-based** is too clever and error-prone: hard to reason about, hard to audit, encourages overly-broad patterns.
- **Deny-by-default with consent prompts** matches the rest of Samuel's UX (signed-by-default, capability-declared) and gives users an audit trail: the consent prompt logs what was asked, the user-decision, and persists the decision per (plugin, host) tuple.

Per-call consents are persisted in `~/.samuel/policy/network.toml` so users aren't re-prompted forever. The `samuel policy reset` subcommand clears them.

## Requirements

### Functional

1. **Container runtime detection** (`internal/plugin/oci/runtime.go`):
   - `Detect()` returns the first available runtime in order: rootless Podman → root Podman → Docker → ErrNoRuntime.
   - `SAMUEL_RUNTIME` env var overrides detection (values: `podman`, `podman-root`, `docker`).
   - `samuel doctor` surfaces the resolved runtime: `container runtime: podman (rootless)`.
   - If no runtime is detected, OCI-tier commands fail with a structured error pointing at `docs/concepts/oci-runtime.md` (new).

2. **OCI plugin loading** (`internal/plugin/oci/plugin.go`):
   - Manifest schema additions in `[runtime]`: `image` (OCI ref), `entrypoint` (string or list), `workdir` (path inside container).
   - Manifest validation: `image` is mandatory for `kind = "oci"`; `image` must be a valid OCI reference with a digest pin (e.g. `ghcr.io/samuelpkg/foo@sha256:...`).
   - Image pull on install: `podman pull` / `docker pull` invoked via the resolved runtime; pull failure surfaces a structured error.
   - Signature verification (PRD 0008): if signed, verify identity against `samuel.toml` patterns; if unsigned and `--allow-unsigned` not set, fail.

3. **Capability enforcement at OCI boundary** (`internal/plugin/oci/runtime.go`):
   - **Filesystem**: each `[capabilities.filesystem]` entry is mapped to a `-v` mount; read-only by default unless explicitly `write = true`.
   - **Env**: only env keys in `[capabilities.env]` are passed into the container; everything else stripped.
   - **Network**: per the Decision section — deny-by-default at container creation (`--network=none` or equivalent); allowed hosts are exposed via a userspace proxy that the container talks to via a unix socket mount. Proxy enforces the allowlist + consent flow.
   - **Resource limits**: per-plugin `[runtime] cpu_quota`, `[runtime] memory_limit` translated to runtime flags.

4. **Network policy + consent flow**:
   - Allowlist: hosts declared in `[capabilities.network] allowed_hosts = [...]` in the manifest. Auto-allowed.
   - Unallowlisted host: when a plugin tries to reach a host not on the allowlist, the proxy intercepts and prompts (`samuel policy prompt: plugin foo wants to reach api.example.com. [a]llow once / [A]lways allow / [d]eny / [D]eny forever?`).
   - Consent persistence: `~/.samuel/policy/network.toml` keyed by `(plugin, host)`.
   - Audit log: every consent decision (and every blocked attempt) logged to `~/.samuel/policy/audit.log` with timestamp.
   - `samuel policy list` lists current consents.
   - `samuel policy reset` clears all consents (with confirmation).
   - `samuel policy reset --plugin foo` scoped clear.
   - JSON mode (`samuel policy list --json`) for tooling.

5. **Agent containerization** (`internal/sandbox/sandbox.go`):
   - `runOCI` complete: builds command-line for Podman/Docker, mounts repo workdir, injects env per adapter's `EnvAllowlist`, joins network only when explicitly allowed.
   - `samuel run --sandbox=oci` becomes the default for non-interactive contexts (CI / production); `--sandbox=host` preserved for development.
   - Adapter images: `internal/agents/<adapter>` carries an `image` field pointing at an Anthropic-published / community-published container image per adapter. Default for `claude` adapter: `ghcr.io/anthropic-cli/claude-code:latest` (or whatever the published image is at v2.3 time).
   - Image pin: per-version digest in `samuel.lock` adjacent to the framework version. Updates via `samuel update --agents`.

6. **Reference plugin** (`github.com/samuelpkg/samuel-claude-code-oci`):
   - Manifest declaring `kind = "oci"`, image, entrypoint.
   - Capabilities: filesystem `/workspace` (read+write), env `ANTHROPIC_API_KEY` only, network `["api.anthropic.com"]`.
   - GitHub Actions release flow: build OCI image, sign with cosign, push to GHCR, publish manifest to registry.
   - Functional: `samuel run --plugin=claude-code-oci` invokes Claude Code inside the container against the local workspace.

7. **`samuel new plugin --kind=oci`** scaffolding:
   - Subcommand extension (PRD 0009 introduces `samuel new plugin`; this adds the `--kind=oci` path).
   - Scaffold produces: `samuel-plugin.toml`, `Containerfile` (Podman-native; Docker-compatible), `Makefile` targets (`make image`, `make push`, `make test`), `README.md`, GitHub Actions release template.

8. **Hermetic e2e** (`e2e/hermetic/oci_test.go`):
   - Build tag `e2e_oci` (separate from `e2e` because requires a runtime; auto-skip if `oci.Detect()` returns `ErrNoRuntime`).
   - Tests:
     - `TestOCI_InstallsFromLocalRegistry` — pull from local Podman registry fixture.
     - `TestOCI_InvokesEntrypoint` — start container, capture output, assert match.
     - `TestOCI_CapabilityDeny_NetworkUnallowed` — plugin tries to reach an unallowed host; consent prompt is auto-denied; expect block.
     - `TestOCI_CapabilityDeny_FilesystemOutsideMount` — plugin tries to write outside mount; expect permission-denied.

9. **Live e2e** (`e2e/live/oci_live_test.go`):
   - `TestOCI_Live_InstallReference` — install `samuel-claude-code-oci` from live registry.
   - `TestOCI_Live_AgentContainerizedRun` — `samuel run --sandbox=oci` against the tetris fixture; assert at least one iteration succeeds.

10. **Plugin-authoring documentation** (`docs/plugin-authors/oci.md`):
    - When to choose OCI over WASM or skill (decision matrix).
    - Containerfile authoring tips.
    - Capability declaration walkthrough.
    - Network policy: how the deny-by-default works, how to declare allowlist, how to handle consent prompts in CI (`SAMUEL_POLICY=allow-once` env, `samuel policy preauth` subcommand).
    - Image signing with cosign.
    - Reference plugin link: `samuel-claude-code-oci`.

11. **`samuel doctor` integration**:
    - Reports detected container runtime + version.
    - For installed OCI plugins: image pulled, digest matches manifest pin, runtime can launch a no-op container.
    - Same `--fix` pattern (re-pull image if digest mismatch).

12. **RFD 0011** at `docs/rfd/0011.md`:
    - Title: "OCI plugin tier + network policy (v2.3)."
    - Decision section: deny-by-default + per-call consent.
    - Outcome filled post-implementation.
    - `rfd-index.toml` updated.

### Non-functional

- Agent containerization adds at most 2s startup overhead vs host-exec (measured per-invocation).
- Consent prompts respect `SAMUEL_POLICY=deny-all` env (CI default) and `SAMUEL_POLICY=allow-once` for one-time CI runs.
- Image pull failures retry 3x with backoff; structured error after the third.
- All OCI-tier structured errors carry `DocsURL` pointing at `docs/plugin-authors/oci.md` or `docs/concepts/oci-runtime.md`.
- No bundled images shipped with Samuel binary. Pulls happen on first use.
- Per-plugin disk budget for image cache enforced by Samuel (`~/.samuel/cache/oci/images/`); LRU eviction when budget exceeded.

## Acceptance criteria

- [ ] `Detect()` returns the expected runtime in order; tests cover all three positive cases + the ErrNoRuntime case.
- [ ] `SAMUEL_RUNTIME` env var override works for all three values.
- [ ] `samuel doctor` reports container runtime + version + image cache size.
- [ ] OCI plugin install path works end-to-end against a local registry fixture (hermetic).
- [ ] OCI plugin install path works against `ghcr.io` (live).
- [ ] Capability enforcement: `TestOCI_CapabilityDeny_*` tests all pass.
- [ ] Network policy: deny-by-default; consent prompt fires for unallowlisted hosts; persisted to `~/.samuel/policy/network.toml`.
- [ ] `samuel policy list` / `reset` / `reset --plugin` work; JSON mode supported.
- [ ] `samuel run --sandbox=oci` against the tetris fixture completes at least one iteration with a containerized Claude Code agent.
- [ ] `samuel new plugin --kind=oci --name=hello` produces a buildable scaffold.
- [ ] `samuel-claude-code-oci` published to registry, signed.
- [ ] `docs/plugin-authors/oci.md` + `docs/concepts/oci-runtime.md` + `docs/concepts/network-policy.md` published.
- [ ] `docs/rfd/0011.md` committed and rendered in mkdocs.
- [ ] CHANGELOG v2.3.0 entry committed.
- [ ] v2.3.0-rc.1 → soak 2 weeks (longer because of policy surface) → v2.3.0 tag.

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Consent-prompt UX is intrusive enough that users disable it via `SAMUEL_POLICY=allow-all` | High | Default-deny remains; document the production-CI flow that pre-allowlists in manifest; reserve `allow-all` for interactive dev only and warn loudly |
| Podman rootless on macOS has known network-stack quirks | High | Test on macOS explicitly in `e2e/live/`; document mac-specific workarounds; recommend Lima/Colima where needed |
| Agent containerization breaks existing user authentication flows (rc.16-style) | High | The container path mounts user's auth config (`~/.claude/`) read-only by default; document the auth-mount story prominently; gate the default-sandbox flip behind a user-opt-in for the first minor (v2.3.0); flip the default in v2.4.0 once stable |
| Cosign-signed image verification adds ≥ 5s to install | Medium | Cache verification result keyed on image digest; warm-path verify ≤ 100ms |
| Network proxy (userspace deny-by-default) becomes a bottleneck for plugins with high request rates | Low | Document the proxy's perf characteristics; offer `--policy-mode=manifest-only` (skip proxy, trust manifest allowlist verbatim) for high-throughput plugins after security review |
| GHCR rate limits affect CI runs | Medium | Cache pulls per-CI-runner; use authenticated pulls with a CI-scoped token (no write perms) |
| Image-pinning by digest causes friction with plugin authors who update tags often | Medium | `samuel update --plugins-oci` re-resolves digest from the manifest's `image` ref; documented |
| Container runtime detection fails on systems with both Podman and Docker installed | Low | First-detected wins per declared order; document explicit `SAMUEL_RUNTIME=docker` override |

## Open questions

- **Image cache budget**: default 10 GB? 5 GB? Configurable via `[oci] cache_budget` in `samuel.toml`. Recommend 10 GB default.
- **Pull on install vs lazy pull on first run**: Recommend pull-on-install for predictability; consider `--lazy-pull` as opt-in.
- **Default sandbox flip in v2.3 vs v2.4**: keep host-default in v2.3, ship OCI-default opt-in; flip default in v2.4 after 1 release of soak. Documented.
- **Multi-arch image support**: must reference plugins ship linux/amd64 + linux/arm64 from the start? Yes — both arches required for the official reference plugin. Plugin authors free to ship single-arch with warnings from `samuel doctor`.
- **Container DNS**: use container runtime's default DNS or proxy DNS lookups too? Proxy DNS for safety (otherwise allowlist hosts can be bypassed by raw IP); document the implication.

## Task hints

1. Audit current `internal/plugin/oci/` skeleton; document gaps
2. Implement `Detect()` with the rootless-Podman → Podman → Docker → ErrNoRuntime ordering
3. Honor `SAMUEL_RUNTIME` env var
4. Add `[runtime]` manifest section for OCI (image, entrypoint, workdir, cpu_quota, memory_limit); update validator + JSON schema
5. Implement image pull on install via the resolved runtime
6. Wire signature verification (PRD 0008 dep) into the OCI install path
7. Implement filesystem capability enforcement (volume mounts read-only by default)
8. Implement env capability enforcement (strict allowlist)
9. Implement deny-by-default network mode at container creation
10. Build userspace network proxy that enforces allowlist + consent
11. Implement consent persistence at `~/.samuel/policy/network.toml`
12. Implement audit log at `~/.samuel/policy/audit.log`
13. Implement `samuel policy list / reset / reset --plugin / prompt` subcommands
14. Complete `internal/sandbox/sandbox.go:runOCI`
15. Wire `samuel run --sandbox=oci` end-to-end against the tetris fixture
16. Add `image` field to `internal/agents/<adapter>` configs
17. Pin agent image digests in `samuel.lock`; implement `samuel update --agents`
18. Build `samuel-claude-code-oci` reference plugin
19. Wire reference plugin's GitHub Actions release flow (build, sign, push, publish)
20. Build `samuel new plugin --kind=oci` scaffolding
21. Write `e2e/hermetic/oci_test.go` with the capability-deny suite
22. Write `e2e/live/oci_live_test.go`
23. Update `samuel doctor` for runtime detection + OCI plugin health
24. Draft `docs/plugin-authors/oci.md`
25. Draft `docs/concepts/oci-runtime.md` + `docs/concepts/network-policy.md`
26. Draft `docs/rfd/0011.md`
27. Update `rfd-index.toml`
28. CHANGELOG v2.3.0 entry
29. Tag v2.3.0-rc.1; smoke test
30. After 2 weeks soak: tag v2.3.0; announce
