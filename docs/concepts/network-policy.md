# Network policy

Samuel's OCI tier denies all outbound network by default. Every
HTTP/HTTPS call a containerized plugin makes is brokered by a
userspace proxy that consults the plugin's declared allowlist and
the persistent consent store before letting bytes through. This
document explains the model, the consent flow, and the CI patterns.

## Why deny-by-default

The wiki's open question listed three options for OCI network
policy:

- **Host allowlist only** — too permissive. A manifest can declare
  `github.com` and exfiltrate via DNS lookups, redirects, or sibling
  hosts.
- **Regex-based** — error-prone. Hard to reason about, hard to audit,
  encourages overly broad patterns.
- **Deny-by-default + per-call consent** — matches the rest of
  Samuel's UX (signed-by-default, capability-declared), gives users
  an audit trail.

The third option won. See [RFD 0011](../rfd/0011.md).

## How it works

At container creation time the launcher passes `--network=none` and
bind-mounts a Unix socket at `/samuel-proxy`. The container's HTTP
client libraries see `HTTP_PROXY=unix:///samuel-proxy` and route
every outbound request through the proxy.

For each request the proxy resolves a Decision in this order:

1. **Persisted consent** — look up `(plugin, host)` in
   `~/.samuel/policy/network.toml`. If a row exists, use it.
2. **Manifest allowlist** — hosts listed in
   `[capabilities.network] allowed_hosts` are auto-allowed.
3. **`SAMUEL_POLICY` env mode** — see "CI patterns" below.
4. **Interactive prompt** — last resort. The user picks
   `[a]llow once`, `[A]lways allow`, `[d]eny`, or `[D]eny forever`.

Every Decision writes one row to `~/.samuel/policy/audit.log`
(JSON Lines) so the user always has a record.

### Raw-IP exfil

The proxy never resolves DNS for the container. The host it sees is
whatever the client wrote in the `Host` header (or the `CONNECT`
target). A raw-IP destination never matches a hostname-based
allowlist, so raw-IP exfil is automatically blocked.

## The consent store

Path: `~/.samuel/policy/network.toml`.

```toml
[[entries]]
plugin = "claude-code-oci"
host = "api.anthropic.com"
decision = "allow-always"
first_seen = "2026-05-13T14:32:11Z"
last_seen = "2026-05-13T14:55:09Z"
```

Subcommands:

```bash
samuel policy list                                 # show all entries
samuel policy list --json                          # machine-readable
samuel policy reset                                # clear all (with prompt)
samuel policy reset --plugin claude-code-oci      # scoped clear
samuel policy prompt                               # replay most recent
samuel policy preauth --plugin foo --host bar.example.com --allow
```

## CI patterns

Interactive prompts don't fit CI. `SAMUEL_POLICY` resolves them
automatically:

| Mode               | Behavior                                            | Use case                         |
|--------------------|-----------------------------------------------------|----------------------------------|
| `interactive`      | Default. Real prompts.                              | Local dev.                       |
| `deny-all`         | Auto-deny every prompt. No persistence.             | Locked-down CI (default).        |
| `allow-once`       | Auto-allow for current process. No persistence.    | CI one-off "I trust this run".  |
| `allow-all`        | Auto-allow + persist.                               | Interactive-dev quick yes.       |

Recommended CI flow:

1. Set `SAMUEL_POLICY=deny-all` as the default.
2. Pre-allowlist required hosts with `samuel policy preauth`:

   ```bash
   samuel policy preauth --plugin claude-code-oci --host api.anthropic.com --allow
   ```

3. The persisted consent now beats `deny-all` (persisted decisions
   win in the resolution order), so plugins reach their declared
   hosts without prompts and any drift is blocked.

## Audit log

Path: `~/.samuel/policy/audit.log`. One JSON object per line:

```json
{"time":"2026-05-13T14:55:09Z","plugin":"claude-code-oci","host":"api.anthropic.com","decision":"allow-once","reason":"manifest-allowlist"}
```

Reasons:

- `manifest-allowlist` — host was on `[capabilities.network] allowed_hosts`.
- `persisted` — store already had a decision.
- `samuel-policy-env` — `SAMUEL_POLICY` mode resolved it.
- `user-prompt` — human answered an interactive prompt.
- `preauth` — `samuel policy preauth` injected the decision.
- `no-prompt-available` — fallback deny (CI without env override).

The audit log is append-only. `samuel policy reset` clears the
consent store but leaves the log intact — history matters for
forensics.
