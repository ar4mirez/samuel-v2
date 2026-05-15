# WASM plugins

WASM-tier plugins are TinyGo (or Rust / AssemblyScript — see "Other
toolchains" below) modules executed inside Samuel by
[wazero](https://wazero.io). Everything is pure Go: no host toolchain
to install, no shared libraries, no platform-specific binaries to
ship. Plugins inherit the same install / lock / sign flow as skill-
tier plugins but get real execution.

This document walks an author from zero to a published plugin.

## When to choose WASM

| Tier  | Use when                                          | Cost                                  |
|-------|---------------------------------------------------|---------------------------------------|
| Skill | Pure prompt / behavioral guidance                 | None (no execution)                   |
| WASM  | Code-level checks or transforms                   | TinyGo build, ≤ 50ms cold-start       |
| OCI   | Heavy runtime, native deps, multi-process         | Docker on host, slower install        |

Pick WASM when:

- The plugin needs to read files and produce structured output.
- The plugin has to run on every `samuel run` iteration (latency matters).
- You want one binary that runs on every host without Docker.

## Toolchain

You need:

- **TinyGo** ≥ 0.31 — `brew install tinygo` or
  [download](https://tinygo.org/getting-started/install/).
- **Go** ≥ 1.22 (TinyGo's host).
- A working `samuel` install (v2.2+).

## Zero to published plugin

```bash
# 1. Scaffold
samuel new plugin --kind=wasm --name=hello
cd hello

# 2. Build
make wasm   # → plugin.wasm

# 3. Install locally for testing
samuel install file://$(pwd) --allow-unsigned

# 4. Verify
samuel doctor
```

The scaffold produces:

```text
hello/
├── samuel-plugin.toml                # manifest with [runtime] + capabilities
├── cmd/main.go                        # TinyGo entry point (hello-export)
├── go.mod
├── Makefile                           # make wasm, make test
├── README.md
├── .gitignore
└── .github/workflows/release.yml      # build + cosign sign + publish on tag
```

## Manifest reference

```toml
name = "go-guide-wasm"
version = "0.1.0"
kind = "wasm"
summary = "TinyGo port of go-guide."

[samuel]
framework = "^2.2.0"
protocol  = "^1.0.0"

[wasm]
module  = "plugin.wasm"
exports = ["lint", "health"]

[runtime]
max_memory   = 64        # MiB; default 64
timeout      = "5s"      # soft deadline (returned to plugin)
hard_timeout = "30s"     # absolute kill via context.Cancel
exports      = ["lint", "health"]   # must match [wasm].exports

[capabilities]
env = ["HOME"]   # empty = no env passed to the guest

[capabilities.filesystem]
read  = ["/workspace"]    # subpaths included
write = []                # write requires explicit grant

[capabilities.network]
hosts = ["api.example.com", "*.cdn.example.com"]
# omit the block → deny-all at the proxy boundary
```

### Reserved export names

Exports must not collide with built-in samuel verbs. The validator
rejects any of: `init`, `install`, `uninstall`, `update`, `search`,
`info`, `ls`, `list`, `run`, `doctor`, `sync`, `version`, `new`.

## Capability model

Every host-side privileged call routes through the per-instance
`Capabilities` snapshot built from your manifest:

- **Filesystem**: paths outside the declared `read` / `write` mounts
  are unmounted; the host returns `permission denied`.
- **Env**: only the keys in `[capabilities.env]` are visible to the
  guest's `wasi.environ_get`. Empty = no env.
- **Network**: deny-by-default. `[capabilities.network].hosts` is the
  allowlist (host-only — port + protocol are not enforced in v2.2).
- **Memory**: capped at `[runtime].max_memory` MiB; allocations
  beyond the cap return a clean error.
- **Timeout**: invocations have a soft deadline (5s default; surfaced
  back to the plugin) and a hard kill via `context.Cancel` at the
  hard-timeout (30s default).

## Cold-start budget

PRD 0009 target: **< 50 ms** median per invocation on a reference
laptop. CI runners are allowed 3x → 150 ms.

What counts as cold-start:

1. wazero compile of the wasm bytes (cached at
   `~/.samuel/cache/wasm-compiled/<sha256>/`).
2. Host module registration (idempotent; counted once per process).
3. Instantiate + WASI snapshot bootstrap.

What does **not** count: the actual `lint()` invocation. Long-running
work belongs in the exported function body — not the module
initialization.

### Measuring

```bash
make wasm-bench    # local benchmark; reports cold + warm medians
```

Or run the bench directly:

```bash
go test -run=^$ -bench=BenchmarkColdStart_TinyGoMinimal \
  -benchtime=10x -count=10 ./internal/plugin/wasm/
```

The CI gate (`.github/workflows/wasm-perf.yml`) fails any PR that
regresses `BenchmarkColdStart_TinyGoMinimal` past the 150 ms median
budget over 10 runs.

### Optimization tips

- Build with `-no-debug -opt=2` (the Makefile scaffold defaults
  to these).
- Avoid `init()` functions in your wasm package; they run on every
  cold-start.
- Keep the SDK surface narrow — TinyGo's stdlib walks every imported
  package at compile time.

## Module cache

Compiled modules are cached at
`~/.samuel/cache/wasm/modules/<sha256>` and reused across invocations
within a `samuel run` loop. Cache hit rate is exposed via
`samuel doctor --json` under `wasm_cache_stats`.

Per `[wasm].cache_budget` in `samuel.toml` (default 500 MiB) the
cache evicts oldest-first when it exceeds the budget.

## Debugging

```bash
samuel run --wasm-debug=stderr   # echoes guest log lines on stderr
```

The `samuel.log(level, msg)` host call always works (no capability
required). Use it generously during bring-up; the framework rate-
limits log volume at 10 lines/second so a stuck loop can't drown the
host.

## Restrictions

TinyGo's `wasi` target imposes:

- No goroutines (single-threaded).
- No `reflect` beyond TinyGo's subset.
- No cgo.
- No `net/http` — use `samuel.net_outbound` (capability-gated).
- `time.Sleep` blocks the runtime; prefer host-side scheduling.

Anything that wants real threads or sockets is OCI-tier territory.

## Other toolchains

TinyGo is the blessed v2.2 toolchain because it matches the plugin-
author base. Rust and AssemblyScript are documented "secondary":
the framework treats `kind = "wasm"` agnostically at the wazero
boundary, so a Rust plugin that compiles with `wasm32-wasi` works,
but no scaffolding ships in v2.2. Rust scaffolding is tracked in
RFD 0012 (post-v2.5).

## Release flow

The scaffold's `.github/workflows/release.yml` does:

1. `tinygo build -o plugin.wasm -target=wasi -no-debug -opt=2 ./cmd`
2. Cosign keyless sign (OIDC, sigstore protobuf-bundle format):

   ```bash
   cosign sign-blob --yes --new-bundle-format \
     --bundle plugin.wasm.bundle plugin.wasm
   ```

3. Upload `plugin.wasm` + `plugin.wasm.bundle` + `samuel-plugin.toml`
   as release assets.

`--new-bundle-format` is required: the framework verifier uses
sigstore-go's `bundle.LoadJSONFromPath`, which only parses the
sigstore protobuf JSON (mediaType
`application/vnd.dev.sigstore.bundle.v0.3+json`). Cosign's legacy
`--bundle` output (`{base64Signature, cert, rekorBundle}`) is
silently rejected and surfaces as `signature bundle missing` at
install.

At install time, the wasm-tier fetcher pulls the release assets
directly via `https://github.com/<owner>/<repo>/releases/download/<tag>/<asset>`
(no git clone needed — the wasm binary lives in the release, not
the tree). See [RFD 0010](../rfd/0010.md) for the fetcher design.

Publishing to the registry is a follow-up PR to
`samuelpkg/samuel-registry`.

## Reference plugin

[samuel-go-guide-wasm](https://github.com/samuelpkg/samuel-go-guide-wasm)
is the production-shaped reference: TinyGo port of the existing
`samuel-go-guide` skill, shared rules package, capability-locked,
performance-tuned. The framework's
`BenchmarkColdStart_TinyGoReference` benches against the published
binary so any framework-side regression is caught before release.

Source lives at
[`samuelpkg/samuel-go-guide-wasm`](https://github.com/samuelpkg/samuel-go-guide-wasm).
