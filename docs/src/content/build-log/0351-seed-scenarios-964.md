---
title: "Seed scenarios (#964)"
date: 2026-10-20
refs: [964]
---

`omniglass seed-dev --scenario <name>` stands up a named fleet instead of always the example one: a
fresh install with nothing seeded, the example fleet with an open alarm on one in every three
components, or the example fleet at enterprise size (about 5,400 components). The synthetic user
(#961) needs these so each exploratory run can start from the situation its mission is about. Every
scenario but the fresh install is the example fleet run through a pure expansion: `Replicate` copies
the fleet's self-contained rooms (found from the fixture, never named in code, so no location type is
keyed on) under replica sites with fleet-unique location names, and `Degrade` reuses the severities and
messages the fixture's own alarms carry. The registry is the CLI help, so the list cannot drift.
Only the example fleet's own devices are ever wired to the simulator, on every run, so a plain re-seed
of a database the large scenario filled does not turn the next `make dev` into a collector load test.
`make dev` takes `SEED_SCENARIO` and the walkthrough stack `STACK_SCENARIO` (every scenario but the
fresh install, which the stack refuses: its node and devices come from the example fleet). Tests: every scenario's
document passes the fixture shape check, the replica and degraded paths are idempotent against a real
Postgres, the fresh install seeds nothing, and the large fleet seeds within a five-minute budget
(2m9s measured).
