# CLI reference

Every command is a Cobra subcommand of `samuel`. Every command supports `--json` for machine-readable output; the envelope is `{ok, data, error}`.

## Global flags

| Flag | Type | Description |
| --- | --- | --- |
| `--json` | bool | Emit JSON envelope to stdout; human-readable output to stderr. |
| `--no-color` | bool | Disable ANSI colour. |
| `--config <path>` | string | Override `samuel.toml` path. |
| `-h, --help` | bool | Show command help. |

## `samuel version`

Show the CLI version, commit, build time, and Go version.

```text
Use:   samuel version
Short: Show CLI version information
```

No flags beyond the global set.

### JSON output

```json
{
  "ok": true,
  "data": {
    "version": "v2.0.0",
    "commit": "abcd123",
    "built": "2026-05-13T10:00:00Z",
    "go": "go1.22.0"
  }
}
```

## `samuel init [project-name]`

Initialise Samuel in the current directory (or `project-name` if given). Writes `samuel.toml`, creates `.samuel/{tasks,builtins,plugins}/`, installs built-in skills, and renders the root `AGENTS.md`.

```text
Use:   samuel init [project-name]
Short: Initialize Samuel in a project
```

| Flag | Description |
| --- | --- |
| `--force` | Re-initialise even if `samuel.toml` already exists. |
| `--minimal` | Skip the default starter plugins. |
| `--yes` | Auto-grant requested capabilities. |
| `--non-interactive` | Fail-closed on prompts (CI use). |

Refuses to run inside the Samuel source repo. Smart bare invocation: already-initialised projects print status and exit 0.

## `samuel doctor`

Check framework + plugin health. Per-plugin pass/fail rendering with summary counts. Suggests translator plugins for assistants found on `PATH`. Detects unmanaged v1 `~/.claude/skills/` content.

```text
Use:   samuel doctor
Short: Check framework + plugin health
```

| Flag | Description |
| --- | --- |
| `--fix` | Re-install plugins whose `Check` reports unhealthy. |

## `samuel sync`

Regenerate per-folder `AGENTS.md`. Walks the project tree, writes one per directory matching `[sync.include]`.

```text
Use:   samuel sync
Short: Regenerate per-folder AGENTS.md
```

| Flag | Description |
| --- | --- |
| `--dry-run` | Print rendered output without writing. |
| `--force` | Overwrite user-customised sections. |
| `--max-depth <n>` | Cap tree walk depth. |

## `samuel install [plugin][@version-range]`

Resolve, fetch, verify, and install a plugin. Bare invocation lists installed plugins and points to `samuel search`.

```text
Use:   samuel install [plugin][@version-range]
Short: Install a plugin
```

| Flag | Description |
| --- | --- |
| `--yes` | Auto-grant requested capabilities. |
| `--allow-unsigned` | Bypass cryptographic signature verification. Use for local plugin development or when installing from a registry whose `signature_bundle` is not yet published. The install line records `Reason: --allow-unsigned` in the lockfile for audit. Note: in v2.1+ this flag now overrides real sigstore-go verification, not a stub. |
| `--allow-prerelease` | Allow prerelease versions during resolution. |
| `--force` | Force reinstall even when already installed. |
| `--dry-run` | Resolve + verify but do not write. |
| `--non-interactive` | Fail-closed on prompts (CI use). |
| `--verbose` | Verbose resolver + fetch logs. |

## `samuel uninstall <plugin>`

Reverse a plugin's mutation log and remove its files.

```text
Use:   samuel uninstall <plugin>
Short: Uninstall a plugin
```

## `samuel ls [plugin]`

List installed plugins. With `--all`, lists everything in the registry the framework can resolve.

```text
Use:   samuel ls [plugin]
Short: List installed plugins
```

| Flag | Description |
| --- | --- |
| `--all` | Include available-but-not-installed plugins. |
| `--type <kind>` | Filter by tier: `skill`, `wasm`, `oci`, `meta`. |

## `samuel search <query>`

Search the registry (cached at `~/.samuel/cache/registries/`, 24h TTL).

```text
Use:   samuel search <query>
Short: Search the plugin registry
```

## `samuel info <plugin>`

Show full manifest detail for a plugin (capabilities, hooks, version range, source).

```text
Use:   samuel info <plugin>
Short: Show plugin manifest detail
```

## `samuel update [plugin]`

Refresh registry caches and update plugins.

```text
Use:   samuel update [plugin]
Short: Refresh registry / update plugins
```

| Flag | Description |
| --- | --- |
| `--all` | Update every plugin to its latest compatible version. |

## `samuel run [methodology]`

Run a methodology (default: `ralph`). Aliases: `rw` → `ralph`. `samuel auto` is a permanent alias for `samuel run start`.

```text
Use:   samuel run [methodology]
Short: Autonomous AI coding loop (Ralph methodology)
```

| Flag | Description |
| --- | --- |
| `--ai-tool <name>` | Agent: `claude`, `codex`, `copilot`, `gemini`, `kiro`. |
| `--max-iterations <n>` | Iteration cap (default 20). |
| `--sandbox <mode>` | `none`, `oci`, `dry-run`. |
| `--sandbox-image <ref>` | Override the OCI image when sandbox = `oci`. |
| `--prd <path>` | Alternate `prd.toon` path. |
| `--methodology <name>` | Alternate methodology (overrides positional arg). |
| `--iterations <n>` | Override `--max-iterations` per call. |
| `--dry-run` | Skip the agent call; run every other hook. |
| `--profile` | Print per-hook timings. |
| `--yes` | Auto-grant prompts during the loop. |

### `samuel run init`

Initialise `.samuel/run/`.

### `samuel run start`

Begin or resume the autonomous loop. Same flags as `samuel run`.

### `samuel run status`

Show loop status. Flags: `--tail <n>` to print the last N lines of `progress.md`.

### `samuel run pilot`

Initialise pilot mode and start. Flags: `--focus <area>`, `--discover-interval <n>`, `--max-discovery-tasks <n>`, `--discover-only`.

### `samuel run convert <prd-path>`

Convert a markdown PRD to `prd.toon`.

### `samuel run tasks`

List every task. Flags: `--status <pending|in-progress|done|skipped|blocked>`.

### `samuel run done <task-id>`

Mark a task complete. Flags: `--commit-sha <sha>`, `--iteration <n>`.

### `samuel run skip <task-id>`

Mark a task skipped. Flags: `--reason <text>` (required).

### `samuel run reset <task-id>`

Reset a task to pending.

### `samuel run enqueue <title>`

Add a task with auto-generated id. Flags: `--priority`, `--complexity`, `--source`.

### `samuel run task add <task-id> <title>`

Add a task with explicit id (for CI / scripts where deterministic ids matter).

## `samuel plugin`

Plugin authoring + registry administration. Consumed by `samuelpkg/samuel-plugin-release`.

### `samuel plugin validate <path>`

Validate a `samuel-plugin.toml` manifest or a `samuel-registry` `index.toml`.

| Flag | Description |
| --- | --- |
| `--registry <path>` | Treat the argument as a registry index instead of a manifest. |

### `samuel plugin info <path>`

Print a single manifest field.

| Flag | Description |
| --- | --- |
| `--name` | Print `plugin.name`. |
| `--kind` | Print `plugin.kind`. |
| `--version` | Print `plugin.version`. |

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Success. |
| `1` | Generic error (see `--json.error.message`). |
| `2` | Validation error (config, manifest, args). |
| `3` | Capability denied. |
| `4` | Signature verification failed. |
| `5` | Lock contention (another `samuel` process holds the project lock). |

Structured errors carry an `error.code` (`SAM-XXX-NNN`) and `error.docs_url` pointing back to the relevant docs page.
