---
title: TOON evaluation (token-oriented object notation)
type: concept
created: 2026-05-12
updated: 2026-05-12
sources: []
tags: [v2, evaluation, encoding]
---

# TOON evaluation

User proposal: use [TOON](https://github.com/toon-format/spec) instead of JSON for the `samuel run` runtime files.

Spec status (as of May 2026):
- v1.0 released 2025-10-28
- v3.0 current (2025-11-24)
- 5 versions in ~4 weeks — active churn
- 243★ on spec repo, 24.1k★ on reference implementation (TypeScript)
- Active design discussions, breaking changes documented per Keep a Changelog
- Format: YAML-style nesting, JSON-like type semantics, tabular array support (`[N]{fields}:`)

## What TOON optimizes for

**Token efficiency on LLM-read text.** Less punctuation than JSON. Tabular arrays compress repeated-shape data. Indentation replaces braces.

Example — same data, three encodings:

**JSON** (current v1):
```json
{
  "tasks": [
    {"id": "1", "title": "Add auth", "status": "pending", "priority": "high"},
    {"id": "2", "title": "Add tests", "status": "pending", "priority": "medium"}
  ]
}
```

**TOON v3** (tabular):
```
tasks[2]{id,title,status,priority}:
  1,Add auth,pending,high
  2,Add tests,pending,medium
```

For a 60-task `prd.json` (v1 dogfood), TOON probably saves 30-50% of tokens on reads. Real.

## Where it actually helps

Not every file benefits equally. Decision matters per file:

| File | Reader | Writer | TOON benefit | Risk |
|---|---|---|---|---|
| `prd.json` (task state) | Samuel (heavy), agent (light) | Both (agent edits during iteration) | Modest — agent rarely reads full file | **HIGH** — agent must write valid TOON |
| `task-context.md` | Agent (every iteration) | Samuel only | **High** when summary tables | Low — Samuel-only writer |
| `progress-context.md` | Agent (every iteration) | Samuel only | Medium — mostly prose | Low |
| `project-snapshot.md` | Agent (every iteration) | Samuel only | **Highest** — file list + TODO counts are tabular | Low |
| `progress.md` | Agent (append) | Both | Low — line-oriented, already compact | Medium |
| `samuel.toml` | Samuel | Human + Samuel | Zero — humans prefer TOML | n/a |
| `samuel.lock` | Samuel | Samuel | Zero — machine-only | n/a |

## The JSON reliability claim — corrected by user observation

**Initial assumption (rejected by user 2026-05-12)**: "trillions of JSON training examples → AI emits reliable JSON." Therefore prd.json was safer than prd.toon.

**User's lived experience disagrees.** Agent has been emitting broken JSON in prd.json often enough that manual repair is a recurring chore — missing commas, trailing commas, unescaped quotes in titles/descriptions. The custom UnmarshalJSON ([[entities/auto-prd]]) only fixes numeric-ID coercion, not these other malformations.

**The corrected analysis**:

1. **JSON fragility is real and observed.** A single missing comma corrupts the entire file. Atomic save protects against partial writes but not malformed content.

2. **TOON's syntax is more forgiving in failure modes.**
   - Tabular rows are line-oriented. A malformed row affects one task, not the whole file.
   - No mandatory commas between items at the array level. The CSV-like row format eliminates the most common JSON failure (missing/trailing commas in arrays of objects).
   - Indentation-based structure makes errors localized and human-spottable.

3. **Spec churn risk is real but bounded.** Pin the version in `samuel.lock`. Refuse to read incompatible versions. Migration is mechanical when the spec settles.

4. **The format choice doesn't matter if the agent doesn't write the file.** See "the cleaner architectural alternative" below — it's a higher-leverage change than the encoding choice.

## The cleaner architectural alternative

If we care about saving agent tokens, the bigger win is to **stop having the agent write prd.json directly**. Have the agent use Samuel CLI subcommands instead:

```
# Instead of: "edit prd.json and set task 1.2 status to completed"
samuel run done 1.2 --commit-sha $(git rev-parse HEAD)

# Instead of: "edit prd.json and add a new pilot-discovery task"
samuel run enqueue "Add input validation" --priority high --source pilot-discovery
```

These commands already exist in v1 ([[entities/command-tree-v1]]). If the agent uses them via Bash tool:

- Samuel owns the storage format completely.
- Format can be TOON, JSON, Protobuf, anything — agent never sees it.
- Mutations are atomic and validated at the CLI boundary.
- Agent's prompt is shorter (no JSON-editing instructions).

This is **strictly better** than choosing JSON vs TOON for prd.json. It moves the question.

## Recommendation (revised — aggressive TOON adoption) #v2-decision

User direction 2026-05-12: more aggressive on TOON because v1's JSON-emission has been breaking in practice. The recommendation reverses my initial conservative bias.

**For v2.0 launch:**

1. **prd file moves to TOON** as `prd.toon`. The tabular `tasks` array is TOON's native sweet spot. Line-oriented rows survive malformations better than nested JSON.

2. **All Samuel-managed runtime files in `.samuel/run/` use TOON** for structured data:
   - `prd.toon` — task state (was `prd.json`)
   - `project-snapshot.toon` — file inventory, test gaps, large files, TODO counts
   - `task-context.toon` — task brief / summary table (whichever applies per mode)

3. **The agent stops writing prd directly.** This is the **load-bearing change** — encoding becomes a Samuel-internal concern. Agent uses CLI subcommands via Bash tool:
   ```bash
   samuel run done 1.2 --commit-sha $(git rev-parse HEAD)
   samuel run skip 3.4 --reason "blocked on external API"
   samuel run enqueue "Add input validation" --priority high --source pilot-discovery
   ```
   These commands already exist in v1 ([[entities/command-tree-v1]]). The auto-mode prompt ([[entities/auto-prompts]]) gets a rewrite: "Use `samuel run done <id>` instead of editing prd.toon."

4. **Append-only logs stay markdown.** `progress.md` and `progress-context.md` are prose-heavy / line-oriented appends. Already compact. The agent reliably appends a single line. No win from TOON here.

5. **Feature-flag with TOON as default**:
   ```toml
   [methodology.ralph.encoding]
   structured = "toon"   # toon | json. Default toon; fall back if needed.
   progress   = "md"     # md | toon. Default md.
   ```
   Default `toon` for structured files. JSON available as escape hatch for any project that hits TOON-specific issues.

6. **Pin TOON version in `samuel.lock`**. Refuse to read files written under a different major version. The version field travels with each `.toon` file so reads know what spec they're parsing.

7. **Write a Go TOON encoder/decoder if none exists.** The reference impl is TypeScript. v3 spec is small (~28 formatting rules). If no maintained Go library exists by build time, ship our own in `internal/encoding/toon/`. Don't block v2.0 on the external ecosystem.

8. **Failure-mode recovery is part of the implementation.** When the agent (or anything) emits malformed TOON via the unblessed path, Samuel's reader treats it the same way [[entities/auto-prd]]'s custom UnmarshalJSON treats numeric IDs — coerce/repair what's recoverable, log a structured warning, skip the bad row, keep the loop running. Line-oriented format means one bad row doesn't corrupt the file.

**The trade we're making**: spec maturity (TOON v3 just landed) for emit reliability (JSON has been breaking in practice). Pin the version, ship our own encoder if needed, validate on write. The CLI-mutation pattern further insulates us by making the agent's TOON-emission optional.

## The deeper principle

The **encoding choice should follow the read/write split**, not be uniform:

- Files written **by Samuel only** → optimize for agent token cost on read (TOON, markdown, whatever's most compact).
- Files written **by Samuel and agent** → optimize for AI emit reliability (JSON, the lingua franca).
- Files written **by humans** → optimize for human readability (TOML, markdown).
- Files written **by machine only** → optimize for parsing speed and integrity (JSON, anything).

Mix encodings inside `.samuel/run/` according to this rule. Don't force one format on everything.

## v2 decision (resolved 2026-05-12) #v2-decision

- **prd.toon is the source of truth** for the autonomous loop. Replaces prd.json from v1.
- **All structured `.samuel/run/` files use TOON**: prd.toon, project-snapshot.toon, task-context.toon.
- **Progress logs stay markdown**: progress.md (append log) and progress-context.md (curated summary).
- **Agent stops writing prd directly** — uses CLI subcommands. This is the load-bearing decoupling. Encoding becomes a Samuel-internal concern.
- **Default encoding is TOON**; JSON available as escape hatch via `[methodology.ralph.encoding] structured = "json"`.
- **Pin TOON spec version in `samuel.lock`** and embed the version on each `.toon` file.
- **Ship our own Go encoder/decoder if none exists** by build time.
- **Failure-mode tolerance**: line-oriented recovery, malformed-row skip, structured warning, keep the loop alive.

## Open

- **Go library audit.** As of May 2026, the TOON reference implementation is TypeScript. Check `github.com/toon-format/*` for Go implementations. If none, write `samuel/internal/encoding/toon/` (small surface — v3 spec rules fit in ~200 lines of Go).
- **CLI mutation prompt rewrite.** The auto-mode prompts ([[entities/auto-prompts]]) need updating to instruct the agent to use `samuel run done|skip|reset|enqueue` via Bash tool, not edit `prd.toon` directly. Lands in v2.0 alongside TOON adoption.
- **TOON version migration helper.** If TOON v4 lands and breaks the row format, ship a `samuel run migrate-encoding` tool that re-emits the file under the new spec. Same pattern as Cargo.lock format migrations.
- **Cost measurement post-launch.** Benchmark same pilot-mode run with JSON vs TOON. Token-delta data informs whether to push TOON into other surfaces (samuel.lock?) later.
- **Recovery UX.** When TOON parsing finds a malformed row, what does `samuel run status` show the user? `samuel doctor` should flag corrupted rows and offer `--fix` to drop them.

## Related

- [[concepts/pre-computed-context]] — the token-discipline thesis TOON would extend
- [[entities/auto-prd]] — the prd.json data model and AI-output resilience patterns
- [[entities/auto-prompts]] — prompts to update if we move to CLI-mutation
- [[entities/auto-runtime-files]] — the seven runtime files affected
- [[entities/config-format]] — TOML for human config, separate decision

Sources:
- [TOON SPEC v3.0](https://github.com/toon-format/spec/blob/main/SPEC.md)
- [TOON CHANGELOG](https://github.com/toon-format/spec/blob/main/CHANGELOG.md)
- [TOON reference implementation (TypeScript)](https://github.com/toon-format/toon)
