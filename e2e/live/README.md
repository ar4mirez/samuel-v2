# e2e/live/

Nightly drift-detection test tier. Drives the real `samuel` binary
against the public
[`github.com/samuelpkg/samuel-test-registry`](https://github.com/samuelpkg/samuel-test-registry)
to exercise the `source.fetchGit` codepath the hermetic tier
(`file://`) cannot reach.

## Status contract — read this first

> **Live tests are allowed to fail. The PR gate is hermetic.**
>
> The hermetic suite (`e2e/hermetic/`, build tag `e2e`) is the
> blocking signal on every PR. The live tier is a drift detector that
> runs nightly + on manual dispatch. A red nightly opens a tracked
> issue (`label:e2e-live-red`) and rolls forward to the next run —
> nothing blocks merging. This separation is intentional: nightly
> flakes from network hiccups or upstream changes should not paralyze
> day-to-day shipping.

If you are tempted to make a live failure block a PR, the test
probably belongs in the hermetic tier instead. Concretely: if the
failure can be reproduced with `file://` URLs and `testdata/`
fixtures, it is a hermetic test.

## What gets exercised here

- The git-clone-specific fetcher (`source.fetchGit`), including the
  rc.6 v-prefix tag fallback and the rc.9 `.git` strip on install.
- Registry index parsing under real conditions (real HTTPS, real
  TOML pulled from `raw.githubusercontent.com`).
- The install/update/uninstall lockfile lifecycle against real
  fixtures.
- The doctor health-report pipeline for live-installed plugins.

## What does NOT belong here

- Anything reproducible with `file://` fixtures → hermetic.
- Sigstore math verification (PRD 0008 territory).
- WASM / OCI tier installs (PRDs 0009 / 0010).
- Anything dependent on a private remote or auth flow — the test
  registry is public and tests run unauthenticated.

## Running locally

Default (production test registry):

```bash
go test -tags=e2e_live -count=1 -v ./e2e/live/...
```

Against a fork or branch of the test registry:

```bash
SAMUEL_LIVE_REGISTRY_URL=github.com/<you>/samuel-test-registry \
  go test -tags=e2e_live -count=1 -v ./e2e/live/...
```

The harness picks `SAMUEL_LIVE_REGISTRY_URL` up in `TestMain`; an
empty value falls back to `DefaultLiveRegistry` (`github.com/samuelpkg/
samuel-test-registry`).

## Wall-time budget

The full suite has a hard ceiling of **2 minutes**, enforced in
`TestMain`. If your additions push past that budget, profile and trim
before adding more coverage — a slow nightly is a noisy nightly. The
suite prints its measured wall-time on every run so the trend stays
visible in CI logs.

## Flake handling

The harness exposes `retryOnce(t, fn)` for the network-bound parts of
each test (clone, fetch, index parse). The cap is one retry,
intentionally: any failure that only surfaces with two-or-more retries
masks a real bug and belongs in the hermetic tier where the harness
can make it deterministic.

If a test is flakey under one retry, do not stack retries — fix the
underlying source of nondeterminism or move the coverage hermetic.

## Adding a new test

1. Identify the codepath difference that hermetic cannot cover.
   Concretely: which line of `internal/plugin/source/` or
   `internal/plugin/registry/` requires a real git remote?
2. If the fixture matrix in `samuel-test-registry/` doesn't already
   express that scenario, extend it (and update the publish steps in
   `samuel-test-registry/README.md`).
3. Drop a `<area>_live_test.go` file with build tag `e2e_live`. Use
   `withLiveRegistry(t, nil)` for the standard project setup;
   `retryOnce` for any subprocess call that hits the network.
4. Update the table in `e2e/README.md` so the tier matrix stays
   honest.

## Caveats v2.0.1 only

- Fixtures are unsigned. The harness exports
  `SAMUEL_VERIFY_ALLOW_UNSIGNED=1` on every `samuel` invocation. Real
  Sigstore verification of the test fixtures is PRD 0008 work.
- The auto-issue flow in the nightly workflow uses `gh issue create`
  directly; we deliberately avoid third-party actions so the
  surface-area for upstream breakage stays small.
