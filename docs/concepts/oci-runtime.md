# OCI runtime

Samuel's OCI tier shells out to a host-installed container runtime
to pull and launch plugin images. The framework itself does NOT ship
a runtime; you install Podman or Docker yourself.

## Detection order

```
SAMUEL_RUNTIME (env override)  →  podman (rootless)  →  docker  →  ErrNoRuntime
```

`samuel doctor` prints the resolved runtime:

```
✓ oci-runtime — podman (rootless), version 4.9.3, cache 1.2 GB
```

If no runtime is detected, the OCI-tier commands fail with a
structured error pointing here, but the rest of Samuel (skill + WASM
tiers, methodology runner) keeps working.

## Choosing a runtime

| Runtime | Mode      | Recommended for                                           |
|---------|-----------|-----------------------------------------------------------|
| Podman  | rootless  | Default. No daemon, no setuid, runs as the invoking user. |
| Podman  | root      | Edge cases where rootless networking misbehaves.          |
| Docker  | daemon    | When the host already runs Docker for other tooling.      |

Force a specific runtime via `SAMUEL_RUNTIME`:

```bash
export SAMUEL_RUNTIME=podman        # rootless podman
export SAMUEL_RUNTIME=podman-root   # podman run as root
export SAMUEL_RUNTIME=docker        # docker
```

Anything else (e.g. `nerdctl`) is rejected with a structured error —
no silent fallback. Add support by opening an issue.

## Image cache

Pulled images live under `~/.samuel/cache/oci/images/` and inherit
the runtime's storage layout. `samuel doctor` reports the total
on-disk size.

The default per-host budget is 10 GB. Set a different value in
`samuel.toml`:

```toml
[oci]
cache_budget = "5g"
```

LRU eviction kicks in when the budget is exceeded; the oldest unused
image is removed first. Samuel never deletes an image that is named
by an installed plugin's manifest.

## Multi-arch expectations

Reference plugins ship `linux/amd64` and `linux/arm64`. Plugin
authors are free to ship a single arch; `samuel doctor` warns when
the installed-plugin set contains an image without a matching arch
for the current host.

## macOS notes

Rootless Podman on macOS has occasional network-stack quirks (it runs
the rootless namespace inside a QEMU/Lima VM). Use the official
Podman Desktop installer; `samuel doctor` reports the version so you
can confirm the install.

## Errors

| Error                                         | Fix                                                                |
|-----------------------------------------------|--------------------------------------------------------------------|
| `no container runtime found`                  | Install Podman or Docker, then re-run `samuel doctor`.             |
| `SAMUEL_RUNTIME refers to unknown binary`     | Unset the variable or set it to a supported value.                 |
| `image pull failed`                           | Network or registry rate-limit. Retries 3× with backoff; check the printed cause. |
| `oci image digest drift`                      | Manifest digest no longer matches the installed image. `samuel doctor --fix` re-pulls. |
