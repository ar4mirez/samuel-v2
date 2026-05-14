---
prd: "0009"
milestone: "WASM plugin tier"
title: Samuel v2.2 — WASM plugin tier (TinyGo first) + reference plugin
authors:
  - name: ar4mirez
state: Committed
labels: [v2, v2.2, plugins, wasm, wazero, tinygo, capability]
created: 2026-05-13
updated: 2026-05-14
target_release: v2.2.0
estimated_effort: 3 weeks
depends_on: 0008-prd-sigstore-verifier.md
---

# PRD 0009: WASM Plugin Tier (TinyGo first)

## Wiki references

- [[concepts/plugin-format]] — three-tier plugin model (skill / WASM / OCI)
- [[CLAUDE]] — "Blessed WASM toolchain: TinyGo first (Go-native, matches plugin author base). Rust and AssemblyScript secondary."
- [[CLAUDE]] — open question: "WASM cold-start budget target — aim < 50ms per invocation"
- [[concepts/versioning-compatibility]] — capability model declared in manifest, enforced at sandbox boundary
- [[synthesis/v2-rc-cycle-lessons]] — "WASM and OCI plugin tiers — both depend on at least one published plugin of each kind"

## Summary

Complete the WASM plugin tier. The `internal/plugin/wasm/` package was scaffolded in v2.0 (wazero embedded, plugin loader skeleton, fixture test) but has no published plugins and no cold-start guarantee. This PRD finalizes the interface, enforces capability permissions at the WASI boundary, validates cold-start performance against the <50ms budget, and ships one reference WASM plugin (`samuel-go-guide-wasm`) end-to-end through the registry. Output: a credible second tier for plugin authors who want execution without OCI weight.

## Problem statement

v2.0 shipped three documented plugin tiers but only one of them — skills — has end-to-end coverage. WASM is the natural middle tier: heavier than text-only skills (it has Go-level execution) but lighter than OCI (no container runtime, no host Docker dependency, deterministic across platforms).

The v2.0 scaffold did the boring work — wazero embedded, manifest parser knows the `kind = "wasm"` variant, loader skeleton in `internal/plugin/wasm/plugin.go`. What's missing:

1. **Capability enforcement**: the `[capabilities.filesystem]` and `[capabilities.network]` blocks in `samuel-plugin.toml` are parsed but not applied at the WASI boundary. A malicious or buggy WASM plugin could exfiltrate via env vars or write outside its sandbox.
2. **Cold-start budget unenforced**: the wiki targets <50ms but there is no measurement, no benchmark, no CI gate.
3. **No reference plugin**: nothing in the registry exercises the WASM path. Plugin authors who want to write a WASM plugin have no template, no working example, no toolchain documentation.
4. **TinyGo build pipeline absent**: the manifest documents `kind = "wasm"` but the framework has no "samuel-plugin-wasm-template" repo or `samuel new --kind=wasm` scaffolding to help authors get started.

This PRD lands all four. After v2.2, WASM is a first-class tier with at least one shipped example and a documented authoring path.

## Goals

- `internal/plugin/wasm/` capability enforcement complete: filesystem (per-path read/write), env (allowlist), network (deny-by-default, per-host allowlist).
- Cold-start performance: <50ms per invocation on reference laptop, measured by benchmark, gated in CI.
- Reference plugin: `samuel-go-guide-wasm` — a TinyGo port of the existing `samuel-go-guide` skill that exposes a `lint` export callable from `samuel run`.
- `samuel new plugin --kind=wasm` scaffolding: produces a TinyGo-buildable plugin tree with manifest, exports, and a `Makefile` target for `make wasm`.
- Plugin-authoring documentation: `docs/plugin-authors/wasm.md` covering the TinyGo toolchain, capability declarations, exports, debug tips, and the cold-start budget.
- Integration into `e2e/hermetic/` — at least one test installs and invokes a WASM plugin end-to-end (bundled fixture, no network).
- Integration into `e2e/live/` — installs `samuel-go-guide-wasm` from the live registry and invokes the `lint` export.
- RFD 0010 — WASM plugin tier (port from [[concepts/plugin-format]] WASM section + this PRD's decisions).

## Non-goals

- No Rust toolchain. Out of scope for v2.2; documented as "secondary, future" in plugin-authors docs.
- No AssemblyScript toolchain. Same.
- No WASM Component Model adoption. v2.2 uses wazero's WASI Preview 1. Component Model migration is a future PRD (post-v2.5 likely).
- No streaming I/O between plugin and host. Exports take serialized input, return serialized output. Streaming is a future PRD if a real use case appears.
- No hot-reload of plugins during a `samuel run` loop. Plugin reload requires a `samuel run` restart. Tracked separately as RFD 0012 (future).
- No multi-version coexistence (two versions of the same plugin loaded simultaneously). Tracked as RFD 0010 (future).
- No WASM-tier plugins for translator surfaces (e.g. `claude-translator-wasm`). Translator surface stays in `internal/translator/` as built-in or skill-level plugins.

## Requirements

### Functional

1. **Capability enforcement at WASI boundary** (`internal/plugin/wasm/runtime.go`):
   - **Filesystem**: wazero `fs.FS` mount derived from the manifest's `[capabilities.filesystem]` block. Each declared path is mounted read-only or read-write per the manifest. Paths outside the declared list are unmounted (host writes fail with `errors.New("permission denied")`).
   - **Env**: `wazero.RuntimeConfig` exposes only the env keys listed in `[capabilities.env]`. Empty list = no env.
   - **Network**: WASI Preview 1 doesn't expose network natively; plugins requesting network capability get a sandboxed `wasiNetwork` import that proxies to the host with an allowlist enforced at proxy entry. Default: deny-all. Allowed hosts declared in `[capabilities.network] hosts = [...]`.
   - **Memory budget**: per-plugin max memory (default 64 MiB) from `[runtime] max_memory` manifest field.
   - **Time budget**: per-invocation soft timeout (default 5s, hard 30s) from `[runtime] timeout`.

2. **Plugin manifest schema additions** (`internal/plugin/manifest/`):
   - New optional `[runtime]` section for WASM plugins: `max_memory`, `timeout`, `exports = ["name1", "name2"]`.
   - Validator: error if `kind = "wasm"` but no `wasm_module` field; error if exports list is empty; error if any export name collides with built-in samuel verbs.
   - JSON schema update: `internal/plugin/manifest/schema/plugin.v2.2.json`.

3. **Cold-start budget enforcement**:
   - Benchmark at `internal/plugin/wasm/runtime_bench_test.go`:
     - `BenchmarkColdStart_TinyGoMinimal` — minimal TinyGo WASM with one no-op export.
     - `BenchmarkColdStart_TinyGoReference` — full `samuel-go-guide-wasm` from a clean cache.
     - `BenchmarkWarmInvoke` — after module is cached.
   - CI gate: `.github/workflows/wasm-perf.yml` runs benchmarks on PRs touching `internal/plugin/wasm/**`; fails if `BenchmarkColdStart_TinyGoMinimal` exceeds 50ms median over 10 runs.
   - Module cache: compiled wazero modules cached in `~/.samuel/cache/wasm/modules/` keyed by SHA256 of the wasm bytes; cache reused across invocations within a `samuel run` loop.

4. **Reference plugin** (`github.com/samuelpkg/samuel-go-guide-wasm`):
   - TinyGo source at `cmd/main.go` implementing the `lint` export.
   - `samuel-plugin.toml` with `kind = "wasm"`, declared capabilities, runtime budgets.
   - `Makefile` with `make wasm` target invoking TinyGo: `tinygo build -o plugin.wasm -target=wasi ./cmd`.
   - GitHub Actions workflow that builds, signs (cosign keyless OIDC), and publishes to the registry on tag.
   - Functional parity with the existing `samuel-go-guide` skill: same lint rules, same output shape.
   - Performance: lint of a 500-LOC Go file completes in <500ms cold, <100ms warm.

5. **`samuel new plugin --kind=wasm` scaffolding**:
   - Subcommand at `internal/commands/new.go` (new file).
   - `samuel new plugin --name=<name> --kind=wasm` creates a directory `<name>/` with:
     - `samuel-plugin.toml` template with sensible defaults.
     - `cmd/main.go` with a TinyGo hello-export.
     - `Makefile` with `make wasm` and `make test`.
     - `.github/workflows/release.yml` template (signing + registry push).
     - `README.md` template.
   - `--kind=skill` and `--kind=oci` (PRD 0010) similarly scaffolded.

6. **Hermetic e2e**:
   - Add `e2e/hermetic/wasm_test.go`:
     - `TestWASM_InstallsLocally` — local file:// registry serving a precompiled fixture wasm.
     - `TestWASM_InvokeExport` — install, then `samuel run` invokes the export; assert output.
     - `TestWASM_CapabilityDeny_FilesystemEscape` — exported function tries to write outside its mount; install succeeds, invoke errors with permission-denied.
   - Fixture wasm precompiled in `testdata/wasm-fixture/` (committed binary; rebuilt via `make wasm-fixtures` script).

7. **Live e2e**:
   - Add `e2e/live/wasm_live_test.go`:
     - `TestWASM_Live_InstallReference` — install `samuel-go-guide-wasm` from live registry.
     - `TestWASM_Live_InvokeReference` — invoke the `lint` export against a test Go file; assert known output.

8. **Plugin-authoring documentation** (`docs/plugin-authors/wasm.md`):
   - "What is a WASM plugin" + when to choose it over skill or OCI.
   - TinyGo toolchain setup (install, version pin, common gotchas).
   - Capability declaration walkthrough.
   - Exports: signatures, serialization, error handling.
   - Cold-start budget: what counts, how to measure, how to optimize.
   - Sample: full walkthrough building a minimal plugin from `samuel new plugin --kind=wasm` to install + invoke.
   - Reference plugin link: `samuel-go-guide-wasm`.

9. **`samuel doctor` integration**:
   - For installed WASM plugins, doctor checks: module loads, declared exports are present, capability block is internally consistent.
   - Same `--fix` pattern as PRD 0006 / rc.14 for orchestrator plugins.

10. **RFD 0010** at `docs/rfd/0010.md`:
    - Title: "WASM plugin tier — wazero + TinyGo + capability gates."
    - Decision section: why TinyGo first (matches plugin author base), why wazero (Go-native, no CGO, embeddable), why WASI Preview 1 (Component Model deferred until stable).
    - Outcome filled post-implementation.
    - `rfd-index.toml` updated.

### Non-functional

- Module cache hit rate ≥ 95% in a long-running `samuel run` loop (measured by an internal counter exposed via `samuel doctor --json`).
- Wazero pinned to a specific version in `go.mod`; bumps reviewed in security-focused PRs.
- TinyGo version pin in plugin-author docs (current LTS at time of writing).
- All WASM-tier structured errors carry `DocsURL` pointing at `docs/plugin-authors/wasm.md`.
- No regression in skill-tier performance (rerun `e2e/hermetic/` benchmarks pre/post).
- `samuel-go-guide-wasm` binary size ≤ 2 MB after TinyGo build with default flags.

## Acceptance criteria

- [x] Capability enforcement landed; `TestWASM_CapabilityDeny_FilesystemEscape` passes.
- [x] `BenchmarkColdStart_TinyGoMinimal` median ≤ 50ms over 10 runs on reference laptop (~0.65 ms on Apple M1 Max).
- [x] CI gate `wasm-perf.yml` runs and fails on regression beyond budget (150 ms CI / 50 ms reference-laptop budget).
- [~] `samuel-go-guide-wasm` published to the registry, signed under the official identity pattern. *Source tree + release workflow shipped at `examples/samuel-go-guide-wasm/`; publish step is a follow-up owner action (separate repo + first cosign-signed release tag).*
- [~] `samuel install go-guide-wasm` + `samuel run` invokes the `lint` export against a fixture Go file. *Install path proven by the hermetic e2e binary fixture; live install + invoke gated behind `SAMUEL_LIVE_WASM_PLUGIN=1` in `e2e/live/wasm_live_test.go`, lights up once the plugin is in the registry.*
- [x] `samuel new plugin --kind=wasm --name=hello` produces a buildable, runnable scaffold (verified end-to-end during implementation).
- [x] `samuel doctor` correctly reports health for installed WASM plugins (module presence + [wasm].exports vs [runtime].exports drift; cache stats under `--json`).
- [x] `e2e/hermetic/wasm_test.go` passes; `e2e/live/wasm_live_test.go` compiles + skips cleanly until plugin lands in registry.
- [x] `docs/plugin-authors/wasm.md` walks an author from zero to published plugin.
- [x] `docs/rfd/0010.md` committed and rendered in mkdocs (registered in `mkdocs.yml` + `rfd-index.toml` + `docs/rfd/index.md`).
- [x] CHANGELOG v2.2.0 entry committed.
- [~] Sample WASM plugin's binary size ≤ 2 MB. *Enforced at release time by `examples/samuel-go-guide-wasm/.github/workflows/release.yml` (`stat -c%s plugin.wasm` ≤ 2097152); cannot be measured until the TinyGo build runs.*
- [~] v2.2.0-rc.1 → soak 1 week → v2.2.0 tag. *Owner action; CHANGELOG entry + RFD are landed.*

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Wazero version churn introduces breaking API changes | Medium | Pin exact wazero version; update only via security-focused PR |
| TinyGo cold-start exceeds 50ms on slower hardware (CI runners) | High | Measure on CI runners specifically; budget allows for 3x slowdown on CI; budget is "reference laptop median" |
| Capability enforcement leaks (a WASI import escapes the sandbox) | Medium | Audit every WASI import that wazero exposes; write fuzz tests for the network-proxy allowlist |
| TinyGo binary size > 2 MB after optimization | Medium | Use `-no-debug` + `-opt=2` build flags; document size tradeoffs; revise target if needed |
| Reference plugin (`go-guide-wasm`) lags the skill version's lint rules | High | Build a sync script: lint rule definitions are shared between the skill and the wasm port via a Go package both can import |
| WASM plugin authors hit obscure WASI errors with no good debug path | High | Plugin-authors docs include a "debugging WASM plugins" section with `samuel run --wasm-debug=stderr` flag |
| Future WASI Preview 2 / Component Model migration breaks v2.2 plugins | Medium | RFD 0010 explicitly commits to backward compat: v2.2 WASM plugins keep loading after Component Model adoption via a compatibility shim |

## Open questions

- **TinyGo version pin**: track LTS or follow latest? Recommend LTS for v2.2; revisit per minor release.
- **Streaming exports**: a couple of plugin use cases (incremental linting, progress reporting) want streaming. Defer to v2.3 RFD with explicit demand from at least 2 plugin authors.
- **Per-plugin memory accounting**: surface in `samuel doctor` or hide? Recommend surfacing under `samuel doctor --verbose` so operators can right-size budgets.
- **Plugin signature requirement for the WASM tier**: enforce signed-by-default for WASM as for skills? Yes — same policy applies. Documented in `docs/concepts/signing.md`.
- **Network capability granularity**: host allowlist sufficient, or also port + protocol? Recommend host-only for v2.2; revisit if a real plugin needs finer-grained control.

## Task hints

1. Audit current `internal/plugin/wasm/` skeleton; document the gap
2. Define capability-enforcement interface; refactor runtime to use it
3. Implement filesystem capability gate using wazero's `fs.FS` mount
4. Implement env capability gate via `wazero.RuntimeConfig`
5. Implement network capability gate (deny-by-default proxy with host allowlist)
6. Implement memory + timeout budgets
7. Write capability-enforcement unit tests with deliberately-malicious WASM fixtures
8. Build the precompiled `testdata/wasm-fixture/` (committed binary + rebuild script)
9. Write `e2e/hermetic/wasm_test.go`
10. Write `runtime_bench_test.go` with cold-start + warm-invoke benchmarks
11. Wire `.github/workflows/wasm-perf.yml` for CI gating
12. Add `[runtime]` manifest section; update validator + JSON schema
13. Implement module cache at `~/.samuel/cache/wasm/modules/`
14. Build `samuel new plugin --kind=wasm` scaffolding command
15. Write `samuel-go-guide-wasm` reference plugin in TinyGo
16. Wire reference plugin's GitHub Actions release flow (build + cosign + registry push)
17. Publish reference plugin to registry; verify discoverable
18. Write `e2e/live/wasm_live_test.go`
19. Update `samuel doctor` for WASM plugin health
20. Draft `docs/plugin-authors/wasm.md`
21. Draft `docs/rfd/0010.md`
22. Update `rfd-index.toml`
23. CHANGELOG v2.2.0 entry
24. Tag v2.2.0-rc.1; smoke test
25. After 1 week soak: tag v2.2.0; announce
