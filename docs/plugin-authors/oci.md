# OCI plugins

OCI-tier plugins are container images pulled by Podman (rootless first)
or Docker and launched on demand. They give plugin authors the full
flexibility of a Linux userland — any language, any runtime, any
system dependency — at the cost of a host container runtime and
slower install.

This document walks an author from zero to a published plugin.

## When to choose OCI

| Tier  | Use when                                          | Cost                                  |
|-------|---------------------------------------------------|---------------------------------------|
| Skill | Pure prompt / behavioral guidance                 | None (no execution)                   |
| WASM  | Code-level checks or transforms                   | TinyGo build, ≤ 50ms cold-start       |
| OCI   | Heavy runtime, native deps, multi-process         | Podman/Docker on host, slower install |

Pick OCI when:

- The plugin needs a non-Go runtime (Python, Node, JVM, Rust binary, …).
- The plugin spawns subprocesses or needs a real OS interface.
- You are packaging an existing CLI tool (Claude Code, GitHub CLI, …)
  whose distribution channel is already a container image.

If you can ship a WASM plugin instead, do — WASM cold-starts in
milliseconds, OCI cold-starts in hundreds. Save the OCI tier for the
plugins that genuinely need it.

## Toolchain

You need:

- **Podman** ≥ 4 (rootless preferred) OR **Docker** ≥ 20.
- **cosign** for signing.
- Buildx-compatible Docker (or Podman 5+) for multi-arch builds.

Samuel itself does not bundle any of these. `samuel doctor` reports
whichever runtime it detects.

## Scaffold a plugin

```bash
samuel new plugin --kind=oci --name=hello
cd hello
make image
samuel install file://$PWD --allow-unsigned   # local-development install
```

The scaffold produces:

- `samuel-plugin.toml` — manifest with `[oci]` block, capability
  declarations, deny-by-default network allowlist.
- `Containerfile` — Podman-native (Docker-compatible).
- `Makefile` — `make image`, `make push`, `make test`.
- `.github/workflows/release.yml` — multi-arch buildx + cosign keyless OIDC.
- `README.md`.

## Manifest schema

```toml
name = "my-oci-plugin"
version = "0.1.0"
kind = "oci"

[samuel]
framework = "^2.3.0"

[oci]
image        = "ghcr.io/me/my-oci-plugin@sha256:abc..."  # MUST be digest-pinned
entrypoint   = ["/opt/run"]
workdir      = "/workspace"
cpu_quota    = "1.5"
memory_limit = "512m"

[capabilities]
env = ["MY_API_TOKEN"]

[capabilities.filesystem]
read  = ["/workspace"]
write = ["/workspace/out"]

[capabilities.network]
allowed_hosts = ["api.example.com"]
```

### Required fields

- `kind = "oci"` — opts the plugin into the OCI loader.
- `[oci].image` — OCI reference **with a sha256 digest**. Tag-only
  references are rejected at `samuel install` time so the user gets
  bit-identical behavior across machines.

### Optional fields

- `[oci].entrypoint` — overrides the image's default entrypoint.
- `[oci].workdir` — container working directory.
- `[oci].cpu_quota` — passed to `--cpus`.
- `[oci].memory_limit` — passed to `--memory`.

## Capability model

Every capability the plugin needs must be declared. Anything
undeclared is denied at the OCI boundary.

| Capability                    | Effect                                            |
|------------------------------|---------------------------------------------------|
| `filesystem.read = [...]`    | `-v <path>:<path>:ro` mounts.                     |
| `filesystem.write = [...]`   | `-v <path>:<path>` (writable) mounts.             |
| `env = ["KEY"]`              | `-e KEY=...` (host value forwarded).              |
| `exec = true`                | Plugin may spawn subprocesses inside container.   |
| `network.allowed_hosts`      | Deny-by-default; listed hosts auto-allowed.       |

Paths in `filesystem.read` / `filesystem.write` default to
`/workspace/`-relative; absolute non-workspace paths are mounted
verbatim from the host.

## Network policy

The OCI tier denies all outbound network by default. A userspace
proxy intercepts every HTTP/HTTPS request and consults the manifest
allowlist + persisted consent store. Unallowlisted hosts trigger an
interactive prompt:

```
plugin foo wants to reach api.example.com.
[a]llow once  [A]lways allow  [d]eny  [D]eny forever?
```

Decisions persist at `~/.samuel/policy/network.toml`. Reset with
`samuel policy reset` (or `--plugin foo` for scoped clear).

In CI, override the prompt with `SAMUEL_POLICY`:

- `SAMUEL_POLICY=deny-all` — auto-deny (production CI default).
- `SAMUEL_POLICY=allow-once` — auto-allow for the current process
  only (CI one-off mode).
- `SAMUEL_POLICY=allow-all` — auto-allow + persist (interactive-dev
  override).

Script-friendly pre-allowlisting (no prompt at all):

```bash
samuel policy preauth --plugin my-oci-plugin --host api.example.com --allow
```

See [docs/concepts/network-policy.md](../concepts/network-policy.md)
for the full model.

## Image signing

Production releases are signed with cosign keyless OIDC. The
scaffolded `.github/workflows/release.yml` does this for you. The
output is a sigstore protobuf-JSON bundle (mediaType
`application/vnd.dev.sigstore.bundle.v0.3+json`).

Verification is automatic on `samuel install`; users opt out with
`--allow-unsigned`.

## Reference plugin

[`samuel-claude-code-oci`](https://github.com/samuelpkg/samuel-claude-code-oci)
ships Claude Code as an OCI plugin. Copy its layout when starting a
new plugin.
