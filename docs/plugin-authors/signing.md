# Signing

Plugin releases are signed with **Sigstore keyless cosign** via the GitHub Actions workflow identity, and the lockfile + manifest schemas treat signature data as a first-class field. As of **v2.1.0** the install path runs real cryptographic verification via [sigstore-go](https://github.com/sigstore/sigstore-go) — the v2.0 `StubVerifier` is now the test-mode escape hatch (`SAMUEL_VERIFY_STUB=1`), no longer the default.

> The wire format and the lockfile schema are stable across the v2.0 → v2.1 transition. Plugins signed against the test registry on v2.0 verify against the same identity patterns on v2.1; nothing about an existing install needs to change.

## How sigstore-go verification works (v2.1+)

```text
samuel install foo
       │
       ▼
┌───────────────────────────────────────────────────────────────┐
│ 1. resolve foo@version → registry index, lookup repo + bundle │
│ 2. fetch artifact (skill / wasm / OCI digest)                 │
│ 3. ensure TUF trust root (~/.samuel/cache/sigstore/trust-root)│
│      ├── cached?  load on-disk JSON (24h TTL)                 │
│      └── miss?    fetch from tuf-repo-cdn.sigstore.dev        │
│                    (3 retries, exp backoff)                   │
│ 4. load .bundle sidecar (sigstore-go bundle JSON)             │
│ 5. construct sigstore.Verifier with the trusted material      │
│ 6. evaluate Verify(bundle, policy)                            │
│      └─ artifact policy: digest(artifact) == bundle.digest    │
│      └─ identity policy: cert.SAN matches identity_patterns   │
│      └─ Rekor presence + observer timestamps                  │
│ 7. cache the result under ~/.samuel/cache/verify/             │
│ 8. render: "Installed foo@1.0.0 (signed by <identity>)"       │
└───────────────────────────────────────────────────────────────┘
```

Each step is independently observable:

- **TUF root fetch** — `~/.samuel/cache/sigstore/trust-root/<binary-version>/trusted_root.json`. Delete the file to force a refresh; the TTL is 24h.
- **Bundle path** — defaults to `<artifact>.bundle` next to the artifact, or whatever the registry index publishes as `signature_bundle`.
- **Result cache** — `~/.samuel/cache/verify/<digest>[+unsigned].json`. Toggling `--allow-unsigned` re-runs the check (the flag is part of the cache key).
- **Rekor URL on failure** — every signature-failure error includes the Rekor log entry URL so you can inspect the underlying transparency-log record.

## Performance budget

| Path | Budget | Notes |
| --- | --- | --- |
| Cold verify (no cache, includes TUF fetch) | ≤ 3s | First call after a binary upgrade |
| Cold verify (trust root cached) | ≤ 500ms | Bundle parse + sigstore math |
| Warm verify (result cache hit) | ≤ 50ms | Steady-state every-day install |

Benchmarks live in [`internal/plugin/verify/verify_bench_test.go`](https://github.com/samuelpkg/samuel/blob/main/internal/plugin/verify/verify_bench_test.go). The cold-path benchmarks require `SAMUEL_BENCH_NETWORK=1` to avoid coupling unit-test runs to sigstore's availability.

## Test-mode escape hatch (`SAMUEL_VERIFY_STUB`)

When the environment variable `SAMUEL_VERIFY_STUB=1` is set, `verify.Default()` returns the `StubVerifier` instead of the production sigstore backend. The stub still enforces every policy field (`identity_patterns`, `allow_unsigned_for`, `--allow-unsigned`) but does not perform Sigstore math. The stub mode is intended for:

- **CI tests** that should not depend on network availability of `tuf-repo-cdn.sigstore.dev`.
- **Air-gapped environments** where TUF cannot reach upstream; pair with `SAMUEL_TUF_MIRROR` once that env var is wired in v2.2.
- **Local development** of plugins, where the author has not yet uploaded a signature bundle.

When stub mode is active, `samuel install` surfaces a warning on every run (not just `samuel doctor`):

```text
⚠ signature verifier: stub (test mode — SAMUEL_VERIFY_STUB=1 active).
```

This makes the escape hatch hard to miss — a user who has accidentally set the variable in their shell rcfile sees it on every install.

## Trust-root rotation

Sigstore's public TUF repository rotates the trusted-root JSON periodically. Samuel caches the file for 24h keyed by binary version, so:

- A new samuel binary forces a fresh fetch on first use.
- Within a binary's lifetime, the cache refreshes once per day.
- Upstream rotation is honored on the next refresh; users see no manual step.

If TUF fetch fails repeatedly (3 attempts with exponential backoff), the verifier returns a structured error pointing at this page; `--allow-unsigned` is the supported escape hatch for transient network failures, and `SAMUEL_VERIFY_STUB=1` is the supported escape hatch for persistent air-gap scenarios.

## Why keyless

Keyless signing means there's no private key to lose, leak, or rotate. The signer identity is the GitHub Actions workflow that produced the release — a verifiable, immutable URL — and the signature is published to the public Sigstore Rekor transparency log. A reviewer can independently verify *which workflow* signed a plugin, on *which commit*, at *what time*, without trusting Samuel.

## Producing signatures

The [`samuelpkg/samuel-plugin-release`](https://github.com/samuelpkg/samuel-plugin-release) reusable workflow handles signing for you. In your plugin repo:

```yaml
# .github/workflows/release.yml
on:
  push:
    tags: ["v*"]

permissions:
  contents: write     # upload release assets
  id-token: write     # required for keyless OIDC

jobs:
  release:
    uses: samuelpkg/samuel-plugin-release/.github/workflows/release.yml@v1
    with:
      kind: skill     # or wasm / oci
```

On tag push the workflow builds the artifact (tarball for skill, .wasm for wasm, OCI image for oci), signs it via `cosign sign-blob --yes` (skill / wasm) or `cosign sign` (oci), and attaches the bundle to the release.

## Verifying

Samuel runs the policy check on every install. As of v2.1+ this includes full Sigstore signature verification via sigstore-go (see [v2.1 status](#v21-status)). The default policy accepts artifacts whose source identity matches [`samuelpkg`](https://github.com/samuelpkg) (and matching plugin-author orgs, configurable per registry source). The identity check is OR-ed across patterns, per [RFD 0003](../rfd/0003.md) §3:

```toml
# samuel.toml
[security]
# Identity patterns the verifier accepts.
trusted_identities = [
  "https://github.com/samuelpkg/.*/.github/workflows/.*",
  "https://github.com/<your-org>/.*/.github/workflows/.*",
]
# Per-registry allowlist of plugins that may install unsigned.
allow_unsigned_for = []
```

The verify cache lives at `~/.samuel/cache/verify/`, keyed by `samuel` binary version so a framework upgrade re-verifies everything.

## `--allow-unsigned` for local dev

Plugin authors working off a `file://` checkout don't have a signature yet. Pass `--allow-unsigned` to skip verification:

```bash
samuel install file://./my-plugin --allow-unsigned
```

This is the **only** sanctioned bypass. `--allow-unsigned` does not extend to remote installs without an entry in `[security].allow_unsigned_for` — Samuel will reject `samuel install github.com/random/plugin --allow-unsigned` unless the registry is on that list.

## Verification flow

```text
samuel install <plugin>
   ├─ resolve via registry
   ├─ fetch artifact + .bundle (Sigstore signature bundle)
   ├─ cosign verify-blob --bundle …
   │     │
   │     ├─ certificate identity ∈ trusted_identities? → continue
   │     └─ no                                          → SAM-VERIFY-001 error
   ├─ record signature digest in samuel.lock
   └─ proceed to capability prompt + install
```

If verification fails, the plugin is not extracted, not installed, and not cached. The lockfile is not touched.

## v2.1 status

v2.1.0 (this release) flips `verify.Default()` to return the production `SigstoreVerifier` and `verify.IsProduction()` to `true`. The `samuel doctor` advisory now reads:

```text
$ samuel doctor
…
Advisories:
⚠ signature verifier: sigstore-go (production)
```

`samuel install foo` against a signed plugin renders the actual OIDC identity in the success line:

```text
$ samuel install samuel-test-skill-signed
✓ Installed samuel-test-skill-signed@1.0.0 (skill) (signed by https://github.com/samuelpkg/samuel-test-skill-signed/.github/workflows/release.yml@refs/tags/v1.0.0)
  signature: verified (https://github.com/samuelpkg/samuel-test-skill-signed/.github/workflows/release.yml@refs/tags/v1.0.0)
```

On failure, the structured error includes a Rekor log entry URL for debuggability:

```text
Error: signature verification failed for foo
  Cause: identity did not match any pattern (rekor: https://rekor.sigstore.dev/api/v1/log/entries?logIndex=...)
  Fix:   confirm the plugin source matches identity_patterns, or install with --allow-unsigned
  Docs:  https://samuelpkg.github.io/samuel/docs/concepts/signing
```

The implementation lives in [`internal/plugin/verify/sigstore.go`](https://github.com/samuelpkg/samuel/blob/main/internal/plugin/verify/sigstore.go); design rationale and trade-offs are in [RFD 0009](../rfd/0009.md). Tracking issue: [#6](https://github.com/samuelpkg/samuel/issues/6) (closed).
