---
title: "Health is the word for health's result; verdict retires"
date: 2026-11-17
status: Accepted
summary: "A system's, location's or component's result is its health (a system's health is degraded), not its verdict: the verdict and verdicts fields, component_verdict, the System and Reach verdict bodies, list-system-verdicts, the Go and console identifiers and the docs prose follow. Reach's per-endpoint up/down result takes its own word, reachability, and a command's settlement verdict is its settlement. Verdict stays only where it is a judgment of a test (the soak and perf gates, a subscription or target test). The vocabulary table's verdict key becomes health"
refs: [2086, 2029, 2038, 2030]
---

- **Date:** 2026-11-17 | **Status:** Accepted | **Pages:** [glossary](/architecture/glossary/),
  [health](/architecture/health/)
- **Decision:** (#2086.) Health's result is named by the concept it belongs to.
  - A system, a location and a component each **have a health**, one of the ordered values
    `healthy` < `incomplete` < `degraded` < `outage` (`unknown` off-order): "the system's health
    is degraded", `health: degraded` on the wire. **Verdict** retires as the name of that value.
    The value words themselves do not change.
  - **Reach takes its own word, reachability.** An endpoint's up/down result is its
    **reachability** (`reachability: up`), and each rung of the ladder (responsive,
    authenticated) reads up or down the same way. It is not health: it is a different axis (can
    the outpost get through to this endpoint, against does this room work), on a different
    domain (two values, not four ordered ones), on a different owner (an endpoint, not a
    component, system or location), and it is an **input** to health, reaching it only through
    the alarm the `reachability-lost` rule raises. One word for both would put "the endpoint's
    health is down" beside "the component's health is degraded", two scales under one name, and
    undo the distinction [ADR-0050](/architecture/decisions/#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain)
    drew between them (a device is down, a room has an outage). The reach section, the
    reachability panel and the `endpoint-reachable` key already say reachability.
  - **A command's settlement verdict is its settlement.** The field is already `settlement`
    (`none` / `pending` / `settled` / `failed`); only the prose and the Go type
    (`SettlementVerdict`) say verdict.
  - **Verdict stays where it is a judgment of a test**: `omniglass lab soak verdict` and the perf
    and footprint gates' pass or fail, a subscription's test against past alarms (matched / missed),
    a notification target's test send. Those are a verdict in the plain sense, a ruling on
    evidence, and none of them is a health value.
  - **The vocabulary table's `verdict:` key becomes `health:`** (with its `Verdicts()` accessor
    and the guard that every health value is a term), and a **health** term carries the retired
    word, so the denylist that rides the table bans it in console strings and docs prose with
    the kept senses above in its Allow pattern. Until the rename lands the key keeps its name;
    this decision already rewrites the four value terms' definitions and the system term's to
    say health.
  - The renames are one follow-up issue under the taxonomy epic. Surfaces that change:
    - **API:** the `verdict` field on ten health bodies (`FleetDotBody`, `FleetHealthOutputBody`,
      `FleetLocationBody`, `FleetSystemBody`, `HealthSystemBody`, `HealthTransitionBody`,
      `ListAlarmsOutputBody`, `PlatformOutpostBody`, `PlatformServerBody`, `PlatformSystemBody`)
      and on the bulk read's entry becomes `health`; `component_verdict` becomes `component_health`;
      `SystemVerdictBody` and `SystemVerdictsOutputBody` become `SystemHealthEntryBody` and
      `SystemHealthListOutputBody`, its `verdicts` list `systems`; the operation
      `list-system-verdicts` becomes `list-system-health`. On reach, the endpoint's `verdict`
      becomes `reachability` and `ReachVerdictBody` becomes `ReachReadingBody` (the same body
      also carries the `responsive` and `authenticated` rungs, so it names a reading, not the
      overall result). Route paths do not change: `GET /systems:health` and the two
      `/{name}/health` explanations already say health.
    - **Schema:** none. The recorded series is already the `og.health` property, and no column
      is named verdict; the word is only in migration comments, which stay (an applied migration
      is never edited).
    - **Permissions:** none. Health reads are gated by `system:read`, `location:read` and
      `component:read`; no permission names verdict.
    - **CLI:** the generated commands follow the OpenAPI rename; `lab soak verdict` stays.
    - **Console:** the outline filter key `verdict` becomes `health` (the outline's own
      `SystemRef` already calls the field `health`), the reachability panel's "verdict" label
      becomes "reachability", and the health history strip, the system and platform hints and
      the webhook row's tip follow; the `Verdict` type and `verdict*` helpers in `web/src/lib`
      follow.
    - **Go:** `health.Verdict` becomes `health.Value` (inside the package the word is already
      health), with `ParseVerdict`, `SystemVerdict`, `SystemVerdictWith`, `ComponentVerdict` and
      `ImpactVerdict` following; the storage reads (`SystemVerdicts`, `ComponentVerdict`,
      `locationVerdict`, the fleet view's `recordedVerdict`) and the reach helpers
      (`EndpointVerdict`, `ladderVerdict`, `probeVerdict`, `AuthVerdict`) take health and
      reachability respectively.
    - **Docs:** the health, collection, API and glossary pages and the guides, swept by the
      denylist; decision entries stay as written.
- **Context:** ADR-0050 framed health as "a verdict", and the word spread from there to every
  surface. It is a courtroom word nobody uses for gear, and the architect found it the oddest
  word in the health model; the operator's own phrasing is "the room's health is degraded", the
  page is Health, the routes end in `/health`, the stored property is `og.health`, the console's
  badge, history and panel are `HealthBadge`, `HealthHistory` and Health history
  ([ADR-2038](/architecture/decisions/#adr-2038-a-rule-fires-an-alarm-raises-incident-is-the-itsms)),
  and the outline already names the field `health`. Every surface already says health for the
  concept and verdict only for its value, so the rename removes a word rather than adding one.
  Counted case-insensitive at the outpost sweep's head (lines, keyword-classified by sense, so
  approximate): **API** (openapi.yaml) 80, of them 57 health, 19 reach, 4 settlement, with 12
  `verdict` fields (11 health, one reach), one `verdicts`, one `component_verdict`, three schema names and one
  operation id; `internal/api` 245 (143 / 83 / 10, 9 test judgment). **Schema:** 13 lines, all
  migration comments, no column or table. **Permissions:** 0. **CLI** (`internal/cli`): 48, of
  them 35 the soak and perf gates' judgment and 13 generated help. **Console** (`web/src`):
  683, 528 health and 146 reach, about ten of them strings an operator reads. **Docs**
  (`docs/src`): 661, 433 health, 179 reach, 30 settlement, 19 test judgment. **Go** beyond the
  API: storage 528, the outpost 297 (mostly reach), health 79, collection 115, vocab 24.
  Considered and rejected for the health value: **status** (it collides with a decision's,
  the board's, HTTP's, a command's and an alarm's status), **state** (too generic, and desired
  state is [ADR-2037](/architecture/decisions/#adr-2037-desired-device-state-is-a-declared-value-override-is-the-catalogs)'s),
  **condition** (retired by ADR-2038). For reach, **health** (above) and **status** (the same
  collision) were rejected in favor of the word its section already uses.
