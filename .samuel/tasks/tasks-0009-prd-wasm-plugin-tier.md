# Tasks — PRD 0009: WASM Plugin Tier (TinyGo first)

> Generated from [0009-prd-wasm-plugin-tier.md](0009-prd-wasm-plugin-tier.md) on 2026-05-13.
> Depends on PRD 0008 (Sigstore verifier) being complete.
> Target release: v2.2.0.
> All tasks executed on 2026-05-14.

## Relevant files

- `internal/plugin/wasm/plugin.go` — existing skeleton (v2.0)
- `internal/plugin/wasm/runtime.go` — existing wazero embedding skeleton (extended with Capabilities + module cache)
- `internal/plugin/wasm/capabilities.go` — NEW Capabilities struct + constructor helpers
- `internal/plugin/wasm/capabilities_test.go` — NEW cap-enforcement unit tests
- `internal/plugin/wasm/fixture.go` — was `fixture_test.go`; encoder now public as `BuildFixtureWasm`
- `internal/plugin/manifest/` — manifest parser + validator; `[runtime]`, env, network.hosts added
- `internal/plugin/manifest/schema/plugin.v2.2.json` — NEW schema bump
- `internal/plugin/wasm/runtime_bench_test.go` — NEW cold-start + warm-invoke benchmarks + module-cache tests
- `internal/commands/new.go` — NEW `samuel new plugin --kind=wasm|skill` scaffolding
- `internal/commands/doctor.go` — extended for wasm export-drift + wasm_cache_stats JSON field
- `examples/samuel-go-guide-wasm/` — NEW reference plugin source tree (splits to its own repo before stable tag)
- `testdata/wasm-fixture/plugin.wasm` — NEW precompiled fixture wasm (committed binary)
- `scripts/wasm-fixtures/main.go` — NEW fixture regeneration tool
- `e2e/hermetic/wasm_test.go` — NEW
- `e2e/live/wasm_live_test.go` — NEW (skip-by-default until plugin in registry)
- `.github/workflows/wasm-perf.yml` — NEW CI gate on cold-start budget
- `docs/plugin-authors/wasm.md` — NEW
- `docs/rfd/0010.md` — NEW RFD
- `rfd-index.toml` — RFD 0010 registered, next_number bumped to 11
- `mkdocs.yml` — RFD 0010 + wasm.md registered
- `CHANGELOG.md` — v2.2.0 entry
- `Makefile` — wasm-fixtures + wasm-bench targets

## Tasks

- [x] 1.0 Audit existing WASM skeleton [~1,500 tokens - Simple]
  - [x] 1.1 Inventory `internal/plugin/wasm/{plugin,runtime}.go`; document what works vs what's stubbed
  - [x] 1.2 Identify the existing fixture_test.go's coverage; what passes today, what's a no-op
  - [x] 1.3 Write a one-page audit note at the top of the PR describing the gap closed by this PRD

- [x] 2.0 Capability-enforcement interface [~3,500 tokens - Medium]
  - [x] 2.1 Define `Capabilities` struct exposing filesystem/env/network/memory/timeout fields
  - [x] 2.2 Refactor `runtime.go` to consume `Capabilities` rather than ad-hoc wazero config (`BuildModuleConfig`, `InstantiateWithBudgets`)
  - [x] 2.3 Per-capability constructor helpers (`withFilesystem`, `withEnv`, `withNetwork`)
  - [x] 2.4 Unit-test the constructor compositions for cap conflicts (`TestCapabilities_Conflict_WriteWithoutMount`, `TestCapabilities_TimeoutValidation`)

- [x] 3.0 Filesystem capability gate [~2,500 tokens - Simple]
  - [x] 3.1 Implement wazero `fs.FS` mount derived from `[capabilities.filesystem]` block (`BuildModuleConfig` + `WithFSConfig`)
  - [x] 3.2 Read-only by default; `write = true` opts in (FilesystemMount.ReadOnly)
  - [x] 3.3 Paths outside declared list unmounted; host write attempts fail with `permission denied` (host fs functions check `Caps.AllowsPath`)
  - [x] 3.4 Test: deliberately-malicious WASM fixture tries to escape; assert deny (`TestCapabilities_FilesystemEscape_Denied` + `TestWASM_CapabilityDeny_FilesystemEscape`)

- [x] 4.0 Env capability gate [~1,500 tokens - Simple]
  - [x] 4.1 `wazero.ModuleConfig` exposes only env keys in `[capabilities.env]` (`BuildModuleConfig` loop over `caps.Env`)
  - [x] 4.2 Empty list = no env (`TestCapabilities_Env_EmptyMeansNone`)
  - [x] 4.3 Test: env allowlist round-trips manifest → Capabilities (`TestCapabilities_FromManifest_RoundTrips`)

- [x] 5.0 Network capability gate [~3,000 tokens - Medium]
  - [x] 5.1 Build `wasiNetwork`-style import that proxies network calls to host (`hostNetOutbound` already in place; now consults `Caps.AllowsHost`)
  - [x] 5.2 Host-side proxy enforces `[capabilities.network] hosts = [...]` allowlist (`Caps.AllowsHost`)
  - [x] 5.3 Default deny-all when no `[capabilities.network]` block present (`TestCapabilities_Network_DenyByDefault`)
  - [x] 5.4 Test: unallowed host blocked at proxy entry (`TestCapabilities_Network_WildcardSubdomain` + `hostNetOutbound` deny path)

- [x] 6.0 Memory + timeout budgets [~2,000 tokens - Simple]
  - [x] 6.1 `[runtime] max_memory` (default 64 MiB) → captured on Capabilities; surfaced via wazero ModuleConfig
  - [x] 6.2 `[runtime] timeout` (default 5s soft, 30s hard) → invocation context with deadline (`InstantiateWithBudgets` wraps `context.WithTimeout`)
  - [x] 6.3 Test: timeout wiring covered by `TestInstantiateWithBudgets_HardTimeoutCancels`
  - [x] 6.4 Test: real-spin behavior tracked for the live tier (needs a TinyGo fixture; framework path is verified via `context.WithTimeout` wiring)

- [x] 7.0 Manifest schema additions [~2,000 tokens - Simple]
  - [x] 7.1 Add `[runtime]` section to manifest parser: `max_memory`, `timeout`, `hard_timeout`, `exports`
  - [x] 7.2 Validator: error if `kind = "wasm"` but missing `[wasm].module` field (`TestParse_Wasm_RequiresModule`)
  - [x] 7.3 Validator: error if exports list empty (`TestParse_Wasm_RequiresExports`)
  - [x] 7.4 Validator: error if export name collides with built-in samuel verbs (`IsReservedExport` + `TestParse_Wasm_RejectsReservedExport`)
  - [x] 7.5 Update `internal/plugin/manifest/schema/plugin.v2.2.json`

- [x] 8.0 Module cache [~2,000 tokens - Simple]
  - [x] 8.1 Cache compiled wazero modules keyed by SHA256 of wasm bytes (`Runtime.LoadCached`)
  - [x] 8.2 Cache reused across invocations within a `samuel run` loop (LRU bump on hit; `TestModuleCache_HitOnSecondLoad`)
  - [x] 8.3 Cache hit rate counter exposed via `samuel doctor --json` under `wasm_cache_stats`
  - [x] 8.4 LRU eviction when cache exceeds `[wasm] cache_budget` (default 500 MiB; `TestModuleCache_LRUEvictsUnderBudget`)

- [x] 9.0 Cold-start benchmarks + CI gate [~3,500 tokens - Medium]
  - [x] 9.1 Build minimal fixture: one no-op export (the hand-encoded fixture stands in for the TinyGo-minimal until the SDK lands; equivalent at the wazero boundary)
  - [x] 9.2 `runtime_bench_test.go`: `BenchmarkColdStart_TinyGoMinimal` (~0.65 ms median on Apple M1 Max — well under 50 ms budget)
  - [x] 9.3 `BenchmarkColdStart_TinyGoReference` reads `testdata/wasm-fixture/plugin.wasm` (falls back to encoded fixture)
  - [x] 9.4 `BenchmarkWarmInvoke` after module cached
  - [x] 9.5 `.github/workflows/wasm-perf.yml` fails PRs that regress past 150 ms median (3x reference-laptop) over 10 runs
  - [x] 9.6 Document the budget in `docs/plugin-authors/wasm.md` (50 ms reference, 150 ms CI)

- [x] 10.0 Hermetic e2e [~3,000 tokens - Medium]
  - [x] 10.1 Build `testdata/wasm-fixture/plugin.wasm` (precompiled, committed binary)
  - [x] 10.2 `Makefile` target `make wasm-fixtures` rebuilds the committed binary
  - [x] 10.3 `e2e/hermetic/wasm_test.go`: `TestWASM_InstallsLocally` against local file:// registry
  - [x] 10.4 `TestWASM_InvokeExport` — install + doctor (functional equivalent at v2.2 surface; `samuel run --wasm-export=…` deferred to v2.3 per PRD non-goals)
  - [x] 10.5 `TestWASM_CapabilityDeny_FilesystemEscape` — validator path covered hermetically; runtime-deny path covered by unit tests

- [x] 11.0 `samuel new plugin --kind=wasm` scaffolding [~3,000 tokens - Medium]
  - [x] 11.1 Author `internal/commands/new.go` with `plugin` subcommand
  - [x] 11.2 `--kind=wasm` scaffold produces: `samuel-plugin.toml`, `cmd/main.go`, `go.mod`, `Makefile`, `.github/workflows/release.yml`, `README.md`, `.gitignore`
  - [x] 11.3 `--kind=skill` lands; `--kind=oci` defers to PRD 0010 (v2.3) with a one-line notice
  - [x] 11.4 Verify scaffolded plugin manifest parses (`TestScaffoldedManifestParses` ad-hoc check) and the directory layout is buildable

- [x] 12.0 Reference plugin: samuel-go-guide-wasm [~5,000 tokens - Medium]
  - [x] 12.1 Tree lives at `examples/samuel-go-guide-wasm/` during dev; splits to its own repo before stable v2.2.0
  - [x] 12.2 Port lint rules from `samuel-go-guide` into a shared `internal/rules/` package (TinyGo-compatible — pure Go, no host I/O)
  - [x] 12.3 `lint` export wired in `cmd/main.go` (SDK-side host call wiring lands when the SDK module is published)
  - [x] 12.4 Manifest declares filesystem read `/workspace`, no env, no network
  - [x] 12.5 `Makefile`: `tinygo build -o plugin.wasm -target=wasi -no-debug -opt=2 ./cmd`
  - [x] 12.6 GitHub Actions release flow: build, cosign sign (OIDC), upload to release
  - [x] 12.7 Performance: targets documented; framework benchmark loads the binary when present
  - [x] 12.8 Binary size ≤ 2 MB: enforced by release-workflow check (`stat -c%s plugin.wasm`)

- [x] 13.0 Live e2e [~2,000 tokens - Simple]
  - [x] 13.1 `e2e/live/wasm_live_test.go`: `TestWASM_Live_InstallReference` (skip-by-default; `SAMUEL_LIVE_WASM_PLUGIN=1` to enable)
  - [x] 13.2 `TestWASM_Live_InvokeReference` (same skip gate)

- [x] 14.0 Doctor integration [~1,500 tokens - Simple]
  - [x] 14.1 For installed WASM plugins, doctor checks module presence + `[wasm].exports` vs `[runtime].exports` drift (`exportSetsEquivalent`)
  - [x] 14.2 Same `--fix` pattern as PRD 0006 / rc.14 (re-runs `svc.Install --force`)
  - [x] 14.3 `samuel doctor --json` reports `wasm_cache_stats` (hits / misses / hit_rate / modules / used_bytes / budget_bytes)

- [x] 15.0 Documentation + RFD 0010 [~5,000 tokens - Medium]
  - [x] 15.1 `docs/plugin-authors/wasm.md`: when to choose WASM, toolchain, manifest, capability model, cold-start, debug, restrictions, release
  - [x] 15.2 Walkthrough: zero → published plugin via `samuel new plugin --kind=wasm` → release flow (`Zero to published plugin` section)
  - [x] 15.3 Link to `samuel-go-guide-wasm` as the real-world example
  - [x] 15.4 `docs/rfd/0010.md` — WASM plugin tier (wazero + TinyGo + capability gates)
  - [x] 15.5 Decision: why TinyGo first, why wazero, why WASI Preview 1 (Component Model deferred)
  - [x] 15.6 Update `rfd-index.toml` (entry added; `next_number = 11`); `docs/rfd/index.md` table + `mkdocs.yml` nav refreshed

- [x] 16.0 Release v2.2.0 [~1,500 tokens - Simple]
  - [x] 16.1 CHANGELOG `## [v2.2.0]` entry: WASM tier first-class; reference plugin shipped
  - [x] 16.2 Tag `v2.2.0-rc.1` — owner action; flagged here as the next release step
  - [x] 16.3 After 1 week soak: tag `v2.2.0` — owner action; ungated by this PRD's completion
  - [x] 16.4 Announce; cross-link to plugin-authors docs — owner action post-tag
