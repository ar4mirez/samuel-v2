# e2e/

End-to-end tests for the `samuel` CLI. Two complementary tiers — one
is the PR gate, the other is the drift detector. Tier matrix:

| Tier     | Path             | Build tag  | When                       | Guards                                                                       | Network |
| -------- | ---------------- | ---------- | -------------------------- | ---------------------------------------------------------------------------- | ------- |
| Hermetic | `e2e/hermetic/`  | `e2e`      | Every PR + push to main    | ~80% of the manual sweep; the `source.fetchFile` codepath                    | No      |
| Live     | `e2e/live/`      | `e2e_live` | Nightly + manual dispatch  | The git-fetcher gap (rc.6, rc.9) + general drift against the real registry   | Yes     |

`docs/reference/testing.md` is the user-facing version of this table.

## Hermetic — fast per-PR signal

Deterministic, no network, no live registry. Each test:

1. Builds the `samuel` binary once per `go test` invocation (`TestMain`).
2. Spins up a tempdir + isolated `HOME` per test.
3. Materializes a local `file://` registry pointing at the shared
   `testdata/sample-skill/` payload.
4. Invokes the actual `samuel` binary via `exec.Command`.
5. Asserts on stdout, stderr, exit code, and filesystem state.

Wall-time budget: ~3 seconds on a reasonable laptop.

```bash
go test -tags=e2e ./e2e/hermetic/...
```

### What hermetic covers

Each test block mirrors one block of the manual sweep that shook out
rc.6 → rc.14.

| Block                | Tests | Validates                                                                              |
| -------------------- | ----- | -------------------------------------------------------------------------------------- |
| `translator_test.go` | 5     | rc.4 (built-in mirror), rc.5 (default-on semantics)                                    |
| `install_test.go`    | 8     | rc.3 (registry parser), rc.14 (`--dry-run` honesty), general pipeline                  |
| `update_test.go`     | 4     | rc.7 (cache key + flag parity + verification reason)                                   |
| `doctor_test.go`     | 6     | rc.10 (stub advisory), rc.11 (plugin health), rc.14 (`--fix` repairs)                  |
| `run_test.go`        | 4     | rc.5/rc.8/rc.12 (run-loop init, empty-queue hint, inline-task PRD parser, mutations)   |

### What hermetic *doesn't* cover (and the live tier does)

| Gap                                            | Why hermetic can't                                 | Live tier coverage                        |
| ---------------------------------------------- | -------------------------------------------------- | ----------------------------------------- |
| rc.6 v-prefix tag fallback                     | `file://` goes through `fetchFile`, not `fetchGit` | `TestInstall_VPrefixedTag_Fetches`        |
| rc.9 `.git` strip from clone                   | Same — only triggers in `fetchGit`                 | `TestInstall_StripsDotGit`                |
| Registry index parsing under real HTTPS        | Local TOML is read raw from disk                   | `TestInstall_RegistryIndexParses`         |
| Update path across two git tags                | Hermetic uses a single fixture version             | `TestUpdate_LiveRegistry_BumpsVersion`    |
| Doctor's verdict against a real-fetched plugin | Hermetic plugins never round-tripped through git   | `TestDoctor_LiveInstalledPlugin_HealthOK` |

Remaining gaps neither tier covers yet:

| Gap                        | Coverage path                                                      |
| -------------------------- | ------------------------------------------------------------------ |
| WASM tier installs         | Unit tests + dedicated fixture once a published WASM plugin exists |
| OCI tier installs          | Same + requires a container runtime in CI                          |
| Real Sigstore verification | New e2e tests after PRD 0008 lands sigstore-go                     |

## Live — nightly drift detection

Subset of the install / update / search / doctor / uninstall scenarios,
run against the real
[`github.com/samuelpkg/samuel-test-registry`](https://github.com/samuelpkg/samuel-test-registry).
Catches upstream drift between the CLI and what the registry actually
serves: schema changes, git-fetcher regressions, signature-policy
regressions on real fetched artifacts.

```bash
go test -tags=e2e_live -count=1 -v ./e2e/live/...
```

To point at a fork or local mirror:

```bash
SAMUEL_LIVE_REGISTRY_URL=github.com/<you>/samuel-test-registry \
  go test -tags=e2e_live -count=1 -v ./e2e/live/...
```

Wall-time budget: 2 minutes total (enforced in `TestMain`). One retry
allowed per network-bound assertion via `retryOnce`.

The full contract — including the "live is allowed to fail; PR gate is
hermetic" rule — lives in [`e2e/live/README.md`](live/README.md). Read
it before touching this tier.

## Adding a new test

The hermetic harness (`helpers_test.go`) gives you four things:

```go
p := newProject(t)              // fresh tempdir + isolated HOME, `samuel init` run
p.setupRegistry("foo", "1.0.0") // local file:// registry with one fixture plugin
out := p.mustSamuel("install", "foo")    // run + fail-on-error
out, err := p.samuel("install", "foo")   // run + return error for assertion
```

The live harness mirrors that shape:

```go
p := withLiveRegistry(t, nil)             // tempdir + HOME, samuel.toml points at the live registry
out, err := p.samuel("install", "foo")    // same signature; SAMUEL_VERIFY_ALLOW_UNSIGNED is set
err := retryOnce(t, func() error { … })   // one-retry wrapper for transient network failures
```

Both harnesses share `assertContains` / `assertNotContains` and the
project's `readFile` / `writeFile` / `rmFile` / `fileExists` helpers.

Every test file starts with `//go:build e2e` (hermetic) or
`//go:build e2e_live` (live), so a default `go test ./...` ignores
them entirely. Don't forget the build tag in new files.

## Where to add what

Use this flowchart when deciding which tier a new test belongs in:

```text
Does the bug reproduce against a file:// registry?
├─ Yes → hermetic (e2e/hermetic/)
└─ No, only against a real git remote
   └─ Add a live test (e2e/live/), and either:
      ├─ extend the fixture matrix in samuel-test-registry/, or
      └─ reuse an existing fixture
```

When in doubt: **prefer hermetic.** Live tests are expensive (network,
nightly cadence, harder to debug). They should exist only for
behaviors that fundamentally cannot be reproduced offline.

## How to add a fixture (live tier)

The live tier's fixture plugins live in the
[`samuel-test-registry/`](../samuel-test-registry/) directory of this
repo — that tree is the source-of-truth for the external public
registry repo. See
[`samuel-test-registry/README.md`](../samuel-test-registry/README.md)
for the publish flow. The contract:

1. Add a new directory under `samuel-test-registry/fixtures/<name>/`
   with at minimum `samuel-plugin.toml` + `SKILL.md`.
2. Add a corresponding `[[plugins]]` entry to
   `samuel-test-registry/index.toml`.
3. Push the fixture as its own GitHub repo under `samuelpkg/` and tag
   the release version(s) referenced in the index.
4. Re-publish the registry repo so the index points at the new fixture.
