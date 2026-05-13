# e2e/

End-to-end tests for the samuel CLI. Two layers, distinct purposes.

## Layers

### `e2e/hermetic/` — fast per-PR signal (build tag `e2e`)

Deterministic, no network, no live registry. Each test:

1. Builds the `samuel` binary once per `go test` invocation (`TestMain`).
2. Spins up a tempdir + isolated `HOME` per test.
3. Materializes a local file:// registry pointing at the shared
   `testdata/sample-skill/` payload.
4. Invokes the actual `samuel` binary via `exec.Command`.
5. Asserts on stdout, stderr, exit code, and filesystem state.

**Runs in ~3 seconds on a reasonable laptop.** Hits everything except
the git-fetcher pipeline (file:// URLs route through
`source.fetchFile`, not `source.fetchGit`).

```bash
go test -tags=e2e ./e2e/hermetic/...
```

### `e2e/live/` — nightly drift detection (build tag `e2e_live`, planned)

Subset of the same scenarios, run against the real
`samuel-registry`. Catches upstream drift between the CLI and what
the registry actually serves — the rc.3 `[[plugins]]` parser bug and
the rc.6 v-prefix tag mismatch would have been caught by either
layer, but neither was in any test before we shipped rc.2.

Not yet implemented. Scope outline lives in [Issue #7](https://github.com/samuelpkg/samuel/issues/7).

```bash
# When live tier exists:
go test -tags=e2e_live ./e2e/live/...
```

## What hermetic covers

Each test block mirrors one block of the manual sweep that shook out
rc.6 → rc.14.

| Block | Tests | Validates |
|---|---|---|
| `translator_test.go` | 5 | rc.4 (built-in mirror), rc.5 (default-on semantics) |
| `install_test.go` | 8 | rc.3 (registry parser), rc.14 (`--dry-run` honesty), general pipeline |
| `update_test.go` | 4 | rc.7 (cache key + flag parity + verification reason) |
| `doctor_test.go` | 6 | rc.10 (stub advisory), rc.11 (plugin health), rc.14 (`--fix` repairs) |
| `run_test.go` | 4 | rc.5/rc.8/rc.12 (run-loop init, empty-queue hint, inline-task PRD parser, mutations) |

## What hermetic *doesn't* cover

| Gap | Why | Coverage path |
|---|---|---|
| rc.6 v-prefix tag fallback | file:// goes through `fetchFile`, not `fetchGit` | `internal/plugin/source/source_test.go` (unit) + e2e/live (planned) |
| rc.9 .git strip from clone | Same — only triggers in `fetchGit` | Same as above |
| WASM tier installs | No WASM plugins in the live registry yet | Unit tests + dedicated fixture once a published WASM plugin exists |
| OCI tier installs | Same + requires a container runtime in CI | Same as above |
| Real Sigstore verification | v2.0 ships `StubVerifier`; sigstore-go swap lands in v2.1 | New e2e tests after the swap |

## Adding a new test

The harness (`helpers_test.go`) gives you four things:

```go
p := newProject(t)              // fresh tempdir + isolated HOME, `samuel init` run
p.setupRegistry("foo", "1.0.0") // local file:// registry with one fixture plugin
out := p.mustSamuel("install", "foo") // run + fail-on-error
out, err := p.samuel("install", "foo") // run + return error for assertion
```

Plus filesystem helpers (`p.readFile`, `p.writeFile`, `p.rmFile`,
`p.fileExists`) and rough string-assertion helpers (`assertContains`,
`assertNotContains`).

Every test file starts with `//go:build e2e` so the regular
`go test ./...` ignores them entirely. Don't forget the build tag in
new files.

## When to add a hermetic test

Whenever a bug surfaced by manual testing this session would have been
caught by code that exercises the actual binary, not just the
in-process command runner. Concretely: if the bug was a difference
between what unit tests asserted and what the binary actually printed
to stdout, file paths, or exit codes — that's a hermetic test.

## When to add a live test (when the tier exists)

Whenever the bug only manifests against a real plugin repo or the
real registry index — git-clone semantics, network-fetched signature
data, registry-served TOML shapes. Don't put network dependencies in
hermetic.
