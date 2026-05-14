# Testing — tier matrix

Samuel ships two end-to-end test tiers. Each guards a different
property; together they cover the install/update/uninstall surface
the CLI exposes.

## At a glance

| Tier     | Path             | Build tag  | When                       | Wall-time budget | Network | Blocks PRs? |
| -------- | ---------------- | ---------- | -------------------------- | ---------------- | ------- | ----------- |
| Hermetic | `e2e/hermetic/`  | `e2e`      | Every PR + push to main    | ~3 s             | No      | Yes         |
| Live     | `e2e/live/`      | `e2e_live` | Nightly + manual dispatch  | 2 min            | Yes     | No          |

Read this as: hermetic is the gate, live is the drift detector. A red
live run opens a tracked issue under `label:e2e-live-red` and rolls
forward to the next nightly. Day-to-day shipping never waits on the
nightly.

## Hermetic tier — fast, deterministic

What it does:

- Builds the `samuel` binary once per `go test` invocation.
- Spins up an isolated `HOME` + project tempdir per test.
- Serves a local `file://` registry from `testdata/sample-skill/`.
- Drives the actual binary via `exec.Command`.

What it guards: every codepath that does not require a real git
remote — registry parsing under `file://`, the `--dry-run` contract,
doctor's `--fix` flow, the translator pipeline, the run-loop.

Run locally:

```bash
go test -tags=e2e ./e2e/hermetic/...
```

## Live tier — nightly drift detection

What it does:

- Drives the same `samuel` binary against
  [`github.com/samuelpkg/samuel-test-registry`](https://github.com/samuelpkg/samuel-test-registry),
  a public test-only registry maintained for this purpose.
- Exercises the `source.fetchGit` codepath the hermetic tier cannot
  reach: git clone, v-prefix tag resolution, `.git` strip, lockfile
  digest against real artifacts.
- Allows one retry per network-bound assertion to absorb transient
  failures; consistent two-attempt failures open a tracked issue.

What it guards: the rc.6 (v-prefix tag fallback) and rc.9 (`.git`
strip) bugs that surfaced during the v2 rc cycle, plus general drift
between the CLI and what a real registry serves.

Run locally:

```bash
go test -tags=e2e_live -count=1 -v ./e2e/live/...
```

Point at a fork or local mirror by overriding the registry URL:

```bash
SAMUEL_LIVE_REGISTRY_URL=github.com/<you>/samuel-test-registry \
  go test -tags=e2e_live -count=1 -v ./e2e/live/...
```

## Which tier should new tests go in?

When in doubt, prefer hermetic. The live tier exists for behaviors
that fundamentally cannot be reproduced offline.

Use this decision tree:

```text
Does the bug reproduce against a file:// registry?
├─ Yes → hermetic
└─ No, requires a real git remote
   └─ Live tier, and extend samuel-test-registry/ if needed
```

## CI surface

| Workflow                             | Tier     | Cadence            | Auto-issue? |
| ------------------------------------ | -------- | ------------------ | ----------- |
| `.github/workflows/ci.yml`           | Hermetic | Every PR + push    | No          |
| `.github/workflows/e2e-live.yml`     | Live     | Nightly 05:00 UTC + manual dispatch | Yes (`label:e2e-live-red`) |

## Related references

- Hermetic harness internals + fixture conventions:
  [`e2e/README.md`](https://github.com/samuelpkg/samuel/blob/main/e2e/README.md)
- Live tier full contract + fixture matrix:
  [`e2e/live/README.md`](https://github.com/samuelpkg/samuel/blob/main/e2e/live/README.md)
- Test registry source-of-truth + publish flow:
  [`samuel-test-registry/README.md`](https://github.com/samuelpkg/samuel/blob/main/samuel-test-registry/README.md)
