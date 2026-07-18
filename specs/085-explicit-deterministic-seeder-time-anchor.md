# 085 — Explicit deterministic seeder time anchor

## Depends on

070–072.

## Problem

Pickle's relative-time providers use a versioned fixed framework anchor so a
root seed remains reproducible across machines and dates. That is the correct
default, but it cannot produce a durable development scenario containing
records that are overdue, due today, due tomorrow, recently completed, stale,
and future relative to a chosen demonstration date.

Using `time.Now()` in an application seeder would make those states drift and
correctly triggers `seeder_nondeterministic`. Hard-coded timestamps preserve
reproducibility but stop illuminating date-sensitive application surfaces as
the desired fixture date changes.

## Outcome

Add an explicit time anchor to seed execution. The tuple of scenario source,
root seed, and anchor reproduces the same graph and values. Omitting the anchor
preserves today's versioned Pickle default exactly.

## Command contract

Compiled and forwarding commands accept:

```text
db:seed DemoSeeder --seed 8675309 --as-of 2026-07-18T12:00:00Z
db:seed DemoSeeder --seed 8675309 --as-of 2026-07-18T12:00:00Z --dry-run
migrate:fresh --seed --as-of 2026-07-18T12:00:00Z
```

`--as-of` accepts canonical RFC 3339 timestamps with an explicit offset and is
normalized to UTC. Date-only input, a missing offset, invalid timestamps, and
multiple conflicting values fail before the command opens the database.

The command prints the effective anchor beside the root seed. Automation can
therefore copy both values from a run and reproduce it. Environment mutation
guards apply unchanged.

## Seed context

Expose the effective anchor through `SeedValueContext` as a read-only value and
make every built-in relative date/time provider derive from it. Custom value
and row seeders may derive values from that context without triggering
`seeder_nondeterministic`.

The context must offer small deterministic helpers for common fixture states,
or document direct duration arithmetic clearly. It must not expose an ambient
clock, mutable global, locale-dependent parser, or database time.

Absolute bounded providers remain absolute. Provider behavior that currently
uses Pickle's fixed anchor changes only when an explicit execution anchor is
present.

## Determinism identity

The effective anchor is part of the execution identity and plan metadata, not
an additional random input. Given the same source, root seed, and anchor:

- graph counts and relationships are identical;
- generated timestamps are identical;
- UUID and scalar streams remain stable unless their documented value depends
  on the anchor; and
- unrelated nodes do not reshuffle.

Changing only the anchor may change time-derived values but must not silently
change stable identities. This allows a named development fixture to advance
its calendar while retaining durable resource references where intended.

## Tooling and export

- `db:seed --dry-run` includes the normalized effective anchor.
- `seeders_plan` accepts an optional explicit anchor and returns it in plan
  metadata without opening the database.
- Squeeze treats `ctx.AnchorTime` or its generated equivalent as deterministic
  and continues to reject `time.Now()`, global clocks, and unversioned external
  time sources.
- Standalone exports support the same flag, parsing, context, providers, and
  output without importing Pickle.
- Help, commands, getting-started, Seeder documentation, and release notes use
  one command spelling. Fix any stale `seeders:list` CLI examples while working
  in this surface; the runnable listing command is `db:seed --list`, while MCP
  tool names remain documented as MCP tools.

## Verification

- Unit tests cover parsing, UTC normalization, invalid inputs, omitted-anchor
  compatibility, and stable random substreams.
- Provider tests prove past, today, tomorrow, recent, and future values relative
  to the explicit anchor.
- Command tests cover forwarding CLI, compiled app, `migrate:fresh --seed`,
  dry-run, and help without mutation.
- Squeeze accepts context-derived time and rejects ambient wall-clock use.
- Export tests reproduce byte-equivalent planned values for the same seed and
  anchor.
- `go test ./...` and strict Squeeze pass.

## Acceptance criteria

1. A fixture can deliberately illuminate date-sensitive states for a chosen
   date without using wall-clock time.
2. Seed plus anchor is sufficient to reproduce the result.
3. Omitting `--as-of` preserves existing generated applications and fixtures.
4. Stable non-time identities do not change merely because the anchor changes.
5. Compiled, forwarded, MCP-plan, and exported behavior agree.

## Non-goals

- Making seed runs follow the wall clock automatically.
- Scheduling recurring fixture refreshes.
- Rewriting existing timestamps during repeat execution.
- Time-zone simulation beyond accepting and normalizing an explicit RFC 3339
  instant.
