# Tasks — PRD 0010: OCI Plugin Tier + Network Policy

> Generated from [0010-prd-oci-plugin-tier.md](0010-prd-oci-plugin-tier.md) on 2026-05-13.
> Depends on PRD 0009 (WASM plugin tier) being complete.
> Target release: v2.3.0.

## Relevant files

- `internal/plugin/oci/runtime.go` — existing scaffold; needs Detect() + capability enforcement
- `internal/plugin/oci/plugin.go` — existing skeleton
- `internal/plugin/oci/launcher.go` — existing launcher; needs deny-by-default network + capability mapping
- `internal/plugin/oci/bridge/` — existing bridge directory
- `internal/plugin/manifest/` — extend with OCI-specific `[runtime]` fields (image, entrypoint, workdir, cpu_quota, memory_limit)
- `internal/sandbox/sandbox.go` — `runOCI` to complete; `runHost` preserved for `--sandbox=host`
- `internal/agents/<adapter>/` — add `image` field to adapter configs
- `internal/commands/policy.go` — NEW `samuel policy list / reset / prompt` subcommands
- `internal/commands/new.go` — extend with `--kind=oci`
- `internal/commands/doctor.go` — extend for runtime detection + OCI plugin health
- `samuel-claude-code-oci/` — NEW external repo (reference OCI plugin)
- `e2e/hermetic/oci_test.go` — NEW (build tag `e2e_oci`; auto-skip if no runtime)
- `e2e/live/oci_live_test.go` — NEW
- `docs/plugin-authors/oci.md` — NEW
- `docs/concepts/oci-runtime.md` — NEW
- `docs/concepts/network-policy.md` — NEW
- `docs/rfd/0011.md` — NEW RFD
- `rfd-index.toml` — register RFD 0011
- `CHANGELOG.md` — v2.3.0 entry

## Tasks

- [ ] 1.0 Audit existing OCI skeleton [~1,500 tokens - Simple]
  - [ ] 1.1 Inventory `internal/plugin/oci/{plugin,runtime,launcher}.go` + `bridge/`; document what works vs stubbed
  - [ ] 1.2 Identify which integration tests cover the existing path (`oci_test.go`)
  - [ ] 1.3 Write the audit note at the top of the PR

- [ ] 2.0 Container runtime detection [~3,000 tokens - Medium]
  - [ ] 2.1 `Detect()` implementation: rootless Podman → root Podman → Docker → ErrNoRuntime
  - [ ] 2.2 Probe via `<runtime> version` shell-out; cache result per process
  - [ ] 2.3 Honor `SAMUEL_RUNTIME` env (`podman`, `podman-root`, `docker`)
  - [ ] 2.4 Unit-test all three positive cases + the ErrNoRuntime case using mocked exec
  - [ ] 2.5 `samuel doctor` surfaces resolved runtime: `container runtime: podman (rootless), version 4.x.x`

- [ ] 3.0 OCI manifest schema + image pull [~2,500 tokens - Simple]
  - [ ] 3.1 Add `[runtime]` section: `image` (mandatory, must be `ref@sha256:digest` pinned), `entrypoint`, `workdir`, `cpu_quota`, `memory_limit`
  - [ ] 3.2 Validator: error if `kind = "oci"` but `image` missing or not digest-pinned
  - [ ] 3.3 Update `internal/plugin/manifest/schema/plugin.v2.3.json`
  - [ ] 3.4 Implement image pull via resolved runtime on install
  - [ ] 3.5 Pull retry 3x with backoff; structured error after the third

- [ ] 4.0 Capability enforcement at OCI boundary [~3,500 tokens - Medium]
  - [ ] 4.1 Filesystem: each `[capabilities.filesystem]` entry → `-v` mount; read-only unless `write = true`
  - [ ] 4.2 Env: only declared keys passed via `-e`; everything else stripped
  - [ ] 4.3 Resource limits: `[runtime] cpu_quota` → `--cpus`; `[runtime] memory_limit` → `--memory`
  - [ ] 4.4 Test: OCI fixture writes outside mount; assert permission denied at container

- [ ] 5.0 Network policy: deny-by-default + userspace proxy [~5,000 tokens - Medium]
  - [ ] 5.1 Container creation: `--network=none` by default (runtime-specific equivalent)
  - [ ] 5.2 Userspace proxy: unix-socket-mounted into container; container talks HTTP through it
  - [ ] 5.3 Proxy enforces allowlist from `[capabilities.network] allowed_hosts = [...]`
  - [ ] 5.4 Unallowlisted host → consent prompt: `[a]llow once / [A]lways allow / [d]eny / [D]eny forever?`
  - [ ] 5.5 Consent persistence: `~/.samuel/policy/network.toml` keyed by `(plugin, host)`
  - [ ] 5.6 Audit log: `~/.samuel/policy/audit.log` records every consent decision + every block
  - [ ] 5.7 `SAMUEL_POLICY=deny-all` (CI default) auto-rejects all prompts
  - [ ] 5.8 `SAMUEL_POLICY=allow-once` auto-allows once-per-process (CI one-off mode)
  - [ ] 5.9 Proxy DNS: lookups go through proxy too (otherwise allowlist bypassed via raw IP)
  - [ ] 5.10 Test: malicious fixture tries raw-IP exfil; assert blocked

- [ ] 6.0 `samuel policy` subcommands [~2,500 tokens - Simple]
  - [ ] 6.1 `samuel policy list` — current consents (plugin × host × decision × first-seen)
  - [ ] 6.2 `samuel policy reset` — clear all consents (confirmation prompt)
  - [ ] 6.3 `samuel policy reset --plugin foo` — scoped clear
  - [ ] 6.4 `samuel policy prompt` — replay the most recent unanswered prompt (CI debug aid)
  - [ ] 6.5 `--json` mode for `policy list`
  - [ ] 6.6 `samuel policy preauth --plugin foo --host bar.example.com --allow` — script-friendly allowlist injection for CI

- [ ] 7.0 Agent containerization [~4,000 tokens - Medium]
  - [ ] 7.1 Complete `internal/sandbox/sandbox.go:runOCI`: build CLI for Podman/Docker, mount repo workdir, inject env per adapter's `EnvAllowlist`
  - [ ] 7.2 Network: join only when explicitly allowed via plugin's network capability declaration
  - [ ] 7.3 `samuel run --sandbox=oci` opt-in default in v2.3; flip to default-on in v2.4 after soak
  - [ ] 7.4 `samuel run --sandbox=host` preserved for development
  - [ ] 7.5 Add `image` field to `internal/agents/<adapter>` configs (Claude first: `ghcr.io/anthropic-cli/claude-code@sha256:...`)
  - [ ] 7.6 Pin agent image digests in `samuel.lock` adjacent to framework version
  - [ ] 7.7 `samuel update --agents` re-resolves digests from manifest tags

- [ ] 8.0 Reference plugin: samuel-claude-code-oci [~4,000 tokens - Medium]
  - [ ] 8.1 Create `github.com/samuelpkg/samuel-claude-code-oci` repo
  - [ ] 8.2 `Containerfile` (Podman-native; Docker-compatible) packaging Claude Code
  - [ ] 8.3 Manifest: `kind = "oci"`, image digest-pinned, capabilities = fs `/workspace` rw + env `ANTHROPIC_API_KEY` + network `["api.anthropic.com"]`
  - [ ] 8.4 Multi-arch build (linux/amd64 + linux/arm64) via buildx
  - [ ] 8.5 GitHub Actions release flow: build, cosign sign (OIDC), push to GHCR, publish to registry
  - [ ] 8.6 Functional: `samuel run --plugin=claude-code-oci` against the tetris fixture completes ≥1 iteration

- [ ] 9.0 `samuel new plugin --kind=oci` scaffolding [~2,000 tokens - Simple]
  - [ ] 9.1 Extend the PRD 0009 `samuel new plugin` command with `--kind=oci`
  - [ ] 9.2 Scaffold: `samuel-plugin.toml`, `Containerfile`, `Makefile` (`make image`, `make push`, `make test`), `.github/workflows/release.yml`, `README.md`
  - [ ] 9.3 Verify scaffolded plugin builds: `samuel new plugin --kind=oci --name=hello && cd hello && make image`

- [ ] 10.0 Hermetic e2e [~3,000 tokens - Medium]
  - [ ] 10.1 `e2e/hermetic/oci_test.go` with build tag `e2e_oci`; auto-skip if `oci.Detect()` returns `ErrNoRuntime`
  - [ ] 10.2 `TestOCI_InstallsFromLocalRegistry` — pull from local Podman registry fixture
  - [ ] 10.3 `TestOCI_InvokesEntrypoint` — start, capture output, assert match
  - [ ] 10.4 `TestOCI_CapabilityDeny_NetworkUnallowed` — unallowed host; auto-deny via SAMUEL_POLICY=deny-all; assert block
  - [ ] 10.5 `TestOCI_CapabilityDeny_FilesystemOutsideMount` — write outside mount; assert deny
  - [ ] 10.6 `TestOCI_PolicyPersistence_AlwaysAllow` — first prompt always-allow; second invocation skips prompt

- [ ] 11.0 Live e2e [~2,000 tokens - Simple]
  - [ ] 11.1 `e2e/live/oci_live_test.go`: `TestOCI_Live_InstallReference` — install `samuel-claude-code-oci` from live registry
  - [ ] 11.2 `TestOCI_Live_AgentContainerizedRun` — `samuel run --sandbox=oci` against tetris fixture; assert ≥1 iteration succeeds
  - [ ] 11.3 Skip gracefully if no runtime detected on CI runner

- [ ] 12.0 Doctor integration [~2,000 tokens - Simple]
  - [ ] 12.1 Report runtime + version + image cache size (`~/.samuel/cache/oci/images/`)
  - [ ] 12.2 For installed OCI plugins: image pulled, digest matches manifest pin, runtime can launch a no-op container
  - [ ] 12.3 `--fix` pattern: re-pull image if digest mismatch
  - [ ] 12.4 Image cache LRU eviction when budget exceeded; default 10 GB via `[oci] cache_budget`

- [ ] 13.0 Documentation [~4,500 tokens - Medium]
  - [ ] 13.1 Draft `docs/plugin-authors/oci.md`: when to choose OCI; Containerfile tips; capability decl; network policy; cosign signing; reference plugin link
  - [ ] 13.2 Draft `docs/concepts/oci-runtime.md`: Podman/Docker detect order; SAMUEL_RUNTIME override; image cache; multi-arch expectations
  - [ ] 13.3 Draft `docs/concepts/network-policy.md`: deny-by-default rationale; consent flow; persistence; CI patterns (deny-all / allow-once / preauth)
  - [ ] 13.4 Update `docs/reference/cli.md` with `samuel policy *` subcommands
  - [ ] 13.5 Update `docs/concepts/signing.md` to note OCI plugins sign images (not blobs)

- [ ] 14.0 RFD 0011 [~3,000 tokens - Medium]
  - [ ] 14.1 Draft `docs/rfd/0011.md` — OCI plugin tier + network policy
  - [ ] 14.2 Decision section: deny-by-default + per-call consent (resolution of the open question)
  - [ ] 14.3 Options Considered: host-allowlist-only / regex / deny-by-default — pros/cons each
  - [ ] 14.4 Outcome filled post-implementation
  - [ ] 14.5 Update `rfd-index.toml`

- [ ] 15.0 Release v2.3.0 [~2,000 tokens - Simple]
  - [ ] 15.1 CHANGELOG `## [v2.3.0]` entry: OCI tier first-class; network policy deny-by-default; agent containerization opt-in
  - [ ] 15.2 Tag `v2.3.0-rc.1`
  - [ ] 15.3 After 2 weeks soak (longer because of policy surface): tag `v2.3.0`
  - [ ] 15.4 Announce; cross-link to network-policy + plugin-authors docs; note v2.4 flip plan for default sandbox
