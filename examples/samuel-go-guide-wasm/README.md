# samuel-go-guide-wasm

Reference WASM plugin for Samuel v2.2 (PRD 0009). TinyGo port of the
existing `samuel-go-guide` skill that exposes a `lint` export callable
from `samuel run`.

This tree lives here during the v2.2 dev cycle so the framework
benchmarks (`BenchmarkColdStart_TinyGoReference`) and e2e tests can
reach a known-good plugin without the registry round-trip. Before the
v2.2.0 stable tag the directory is split out to
`github.com/samuelpkg/samuel-go-guide-wasm` and dropped from the
framework repo.

## Capabilities

| Capability       | Value                                |
|------------------|--------------------------------------|
| `filesystem.read`| `/workspace/**/*.go`                 |
| `env`            | (none)                               |
| `network`        | deny-all (no `[capabilities.network]`)|
| `runtime.max_memory` | 64 MiB                            |
| `runtime.timeout`    | 5 s soft, 30 s hard               |

## Build

```bash
tinygo build -o plugin.wasm -target=wasi -no-debug -opt=2 ./cmd
```

Binary-size target: ≤ 2 MB (PRD 0009 acceptance criterion).

## Lint rules

The lint rules live in `internal/rules/`, deliberately shared between
this plugin and the host-side `samuel-go-guide` skill so the two stay
in sync. The shared package is a pure Go package — no host I/O, no
host-only dependencies — so TinyGo can compile it for `target=wasi`.

## Performance targets

- Lint of a 500-LOC Go file: ≤ 500 ms cold, ≤ 100 ms warm
- Cold-start: ≤ 50 ms (covered by framework's
  `BenchmarkColdStart_TinyGoReference`)

## Local validate

```bash
samuel install file://$(pwd)
samuel doctor
samuel run --plugin=go-guide-wasm
```
