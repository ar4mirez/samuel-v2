# RFDs

**RFD = Request for Discussion.** This is the public design record of every major decision in Samuel v2. Each RFD captures the problem, the alternatives considered, the chosen path, and the acceptance criteria for shipping it.

States: *Prediscussion → Ideation → Discussion → Published → Committed* (or *Abandoned*). Anything in `docs/rfd/` is at least *Discussion*; the table below is the source of truth for current state.

The table is regenerated from [`rfd-index.toml`](https://github.com/samuelpkg/samuel/blob/main/rfd-index.toml) by `scripts/gen-rfd-index.sh`. Don't hand-edit the table — edit the TOML and re-run the script.

<!-- RFD_INDEX_START -->

| # | Title | State | Labels |
| --- | --- | --- | --- |
| [0001](0001.md) | Three-tier plugin architecture (skill / WASM / OCI) | Committed | v2, plugin-format, architecture, wasm, oci, sandboxing |
| [0002](0002.md) | AGENTS.md primary, tool-specific files via translator plugins | Committed | v2, agents-md, translator-plugins, agnostic, cross-tool |
| [0003](0003.md) | SemVer, capability model, Sigstore signing | Committed | v2, versioning, security, capabilities, sigstore, manifest |
| [0004](0004.md) | Methodology hooks — default built-in + plugin enhancement | Committed | v2, methodology, hooks, extensibility, ralph |
| [0005](0005.md) | Component lifecycle interface as v2 plugin loader | Committed | v2, plugin-loader, lifecycle, foundation, architecture |
| [0006](0006.md) | samuel run [methodology] — Ralph as default, CLI-mutation pattern | Committed | v2, cli, methodology, ralph, toon, prompts |
| [0007](0007.md) | Plugin migration from v1 skills | Committed | v2, migration, plugins, registry, starter-pack |
| [0008](0008.md) | Drop gstack and gbrain from the v2 framework | Committed | v2, scope, deprecation, clean-break |
| [0009](0009.md) | Plugin signing via Sigstore enforcement (v2.1) | Committed | v2, v2.1, signing, sigstore, security, plugin-verify |
| [0010](0010.md) | WASM plugin tier — wazero + TinyGo + capability gates | Published | v2, v2.2, plugins, wasm, wazero, tinygo, capabilities |

<!-- RFD_INDEX_END -->

## Reading order

If you're new to the v2 design, read in this order:

1. **[0005](0005.md)** — the lifecycle interface that everything else hangs off.
2. **[0001](0001.md)** — why three plugin tiers.
3. **[0003](0003.md)** — SemVer, capabilities, signing.
4. **[0008](0008.md)** — what we dropped and why.
5. **[0007](0007.md)** — how v1 skills became v2 plugins.
6. **[0002](0002.md)** — AGENTS.md as canonical.
7. **[0004](0004.md)** — methodology hooks.
8. **[0006](0006.md)** — the run command surface + CLI-mutation invariant.

## Writing a new RFD

1. Bump `next_number` in `rfd-index.toml`, append an entry with state `Prediscussion` or `Ideation`.
2. Draft the RFD at `.samuel/rfd/NNNN-slug.md` while it's private.
3. Promote to `docs/rfd/NNNN.md` when it reaches *Discussion* state.
4. Re-run `scripts/gen-rfd-index.sh` to refresh this table.
