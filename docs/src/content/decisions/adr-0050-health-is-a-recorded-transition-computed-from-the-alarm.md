---
title: "Health is a recorded transition, computed from the alarm-capability-role chain"
date: 2026-07-21
status: "Accepted"
summary: "Health is **recorded as a transition** and **recomputed at the write**, never on read. An **`alarm`** is component-local and names the **capabilities** it degrades; a component satisfies a role only when it provides every required capability and none of them is degraded; a role below its **quorum** is impaired and contributes its **`impact`** (`outage` / `degraded` / `none`); a system takes the worst of its roles, a location the worst of its systems. The verdict domain is **`healthy` / `degraded` / `outage`** and the judgement is a **pure package** (`internal/health`), unit-tested with no database. The recorded carrier is **`state_datapoint`**, already transition-only, so the history is edges and only edges; **compute-on-read** (no history) and **write-through-on-read** (the edge timestamped when somebody looked) are both rejected. A **read never writes**, and it computes the verdict it serves from the same rows it shows, so a report cannot contradict its own evidence. PR8 of the estate-model shift, closing epic [#266](https://github.com/hyperscaleav/omniglass/issues/266)"
---

- **Date:** 2026-07-21 | **Status:** Accepted | **Pages:** [health](/architecture/health/),
  [core entities](/architecture/core-entities/), [API](/architecture/api/), [glossary](/architecture/glossary/),
  [Standards guide](/guides/admin/standards/), [Work with an entity](/guides/operator/entities/)
- **Decision:** Health is a **verdict** on a system or a location, **derived** from what is wrong with the
  components staffing it and **recorded as a transition** at the moment it changes. Five calls carry that.

  **1. Capability is the routing key, and an alarm is how a component loses one.** An **`alarm`** is
  **component-local** (`component_id`, a `severity` of `info` / `warning` / `critical`, a `message`, a
  `raised_at`, and a **nullable `cleared_at`**), and **`alarm_capability`** names the
  [capabilities](#adr-0044-the-component-classification-catalogs) it degrades. Clearing **keeps the row**, so
  the record of what was wrong and when survives the fix. The chain from there is one sentence per hop: a
  component **satisfies** a role only when it provides **every** required capability and **none of those is
  currently degraded**; a role with fewer satisfying components than its **quorum** is **impaired**; an
  impaired role contributes its declared **`impact`** (`outage` / `degraded` / `none`, a column on
  `system_role`, defaulting to `degraded`); a system takes the **worst** contribution among its roles, and a
  location the worst among the systems placed anywhere beneath it. That chain is why a capability is **flat**
  and why a role requires a **set** of them: capability is the only vocabulary shared by the thing that breaks
  (a component) and the thing that cares (a slot in a room), so it is the only honest place to route through.
  **Impact lives on the role**, not on the alarm or the component, because the same broken box matters
  differently depending on the slot it was filling: a dead confidence monitor is not a dead main display.

  **2. The judgement is a pure package.** **`internal/health`** takes resolved inputs and returns a verdict,
  with **no database**: `Component.Satisfies`, the quorum boundary, worst-wins at both levels, and the
  impact mapping are unit tests, not SQL. Two of its defaults are deliberate **safety** calls in opposite
  directions. An **unrecognized impact reads `degraded`**, so a bad value can never make an impaired role
  silently harmless. An **unrecognized recorded value reads `healthy`**, so one stray row cannot paint an
  estate broken. The rule behind both: fail loud about a **judgement**, fail quiet about a **record**.

  **3. Health is recorded as a transition-only state, on `state_datapoint`.** The requirement this whole
  design serves is an **accurate history of the edges**: exactly when a system stopped working, answerable
  weeks later. `state_datapoint` is already that primitive (the ingest path writes a row only when the value
  differs from the last one stored, and `StateTransitions` reads the ordered flips the reachability
  availability strip draws), so health reuses it rather than adding a history table: the **owner arc**,
  `provenance='calculated'`, and `source_rule='health-rollup'` (the lineage CHECK requires a non-null
  `source_rule`). The first value for an owner is always recorded, even `healthy`, so a reader can tell
  "healthy since we started watching" from "never evaluated".

  **4. Recompute happens at the writes that can change health, in the same transaction.** Every mutation that
  can move a verdict recomputes the affected chain before it commits: **raising** or **clearing** an alarm,
  **assigning** or **unassigning** a component, **declaring** or **withdrawing** a role, changing a role's
  **quorum** or **impact**, changing a component's **capabilities** or its **product**, **creating** a system,
  and changing the **standard** it conforms to or the **location** it sits in (recomputing **both** the old
  and the new location, since the one it left may have just improved). **A read never writes.** Two
  alternatives were considered and rejected, and both fail the same requirement. **Compute-on-read** keeps no
  history at all, so "when did this break" is unanswerable by construction. **Compute-and-write-through-on-read**
  keeps a history that is **sampled by whoever opens a page**: the edge timestamp becomes the moment somebody
  looked, not the moment the estate changed, and an estate nobody watched over a weekend has no weekend. A
  transition is only worth recording if it is recorded **where the change happened**.

  **5. A report computes the verdict it serves from the evidence it shows.** The health report originally
  served the **last recorded** verdict while resolving the contributing roles **live**, which let a system
  with nothing recorded yet report `healthy` beside an impaired `outage` role: the report contradicted
  itself. The served verdict is now derived from the same resolved rows the report displays, so the headline
  and the reason can never disagree. This is **not** self-healing on read: nothing is written, and the
  **recorded transitions remain the source for history**. A missing trigger can therefore cost an edge in the
  history, but it can never make a report lie about the present.

  **Routes**, regenerated into the OpenAPI document, the cobra CLI, and the typed client:
  `GET` / `POST /components/{name}/alarms` and `DELETE /components/{name}/alarms/{id}` (gated
  `component:read` / `:update`); `GET /systems/{name}/health` and `GET /locations/{name}/health` (gated
  `system:read` / `location:read`, scope-injected, an out-of-scope owner a non-disclosing 404), each returning
  the verdict, the contributing roles (with the degraded capabilities and the causing alarms) or the systems
  beneath, and the recorded transitions over the last 30 days. The CLI reads
  `omniglass component alarms|raise-alarm|clear-alarm`, `omniglass system health`, and
  `omniglass location health`. The seed adds a **`health`** state-kind property
  (`healthy` / `degraded` / `outage`) so the recorded series is typed like any other.
- **Context:** The architect's requirement was stated plainly: *"The most important thing about health is that
  we have a real, accurate history of the edges. We need to know exactly when a system went from healthy to
  unhealthy, and be able to look back at it weeks later."* Every call above falls out of taking that
  literally. Once the history has to be **accurate**, the write side is the only correct place to compute
  from, and once the carrier has to be **edges**, `state_datapoint` is already the right table and a new
  `health_history` would have been a second, worse copy of it. Recording an **opening verdict at system
  creation** then surfaced a latent bug the rest of the schema had been quietly carrying: every system now
  had a `state_datapoint` row from birth, and **every rename failed on the owner foreign key**, because those
  FKs address the owner **by name** and declared no `ON UPDATE`. Migration `20260721170000` re-adds all four
  `state_datapoint` owner FKs with **`on update cascade`**, which is what name-as-address always meant: the
  history follows the entity rather than pinning its old name. Health did not create that bug, it made it
  **reachable for every system**, which is the useful kind of forcing function.
- **Deferred:** the **same FK gap** on `metric_datapoint`, `event`, `property_value`, `alarm`, and the role
  tables' name-addressed columns, tracked in
  [#314](https://github.com/hyperscaleav/omniglass/issues/314). An alarm today is **written by an operator or
  an API caller**, not produced by an [`event_rule`](/architecture/alarms-actions/) over datapoints; the rule
  that opens and clears one automatically is the next tier. Also deferred: **system-** and **location-owned**
  alarms (the alarm arc is component-only today), the **`unknown`** verdict with its coverage and staleness
  reasons, the **`global`** estate top, the **SLI / SLO / SLA** family and the **KPI** set, an alarm's
  interaction with **operational mode** (maintenance suppressing a contribution), and **dependency
  suppression**.
- **Supersedes:** three earlier calls on [health](/architecture/health/). (a) **The value vocabulary**:
  [ADR-0003](#adr-0003-health-reads-ok-not-up) named the healthy state `ok` over an ordered
  `ok < degraded < down`; the built domain is **`healthy` < `degraded` < `outage`**, keeping that entry's
  reasoning (name the verdict, not the ping) and changing only the words, since `outage` says what a broken
  room means to the people in it. (b) **Where impact is declared**: the design hung an optional `health`
  impact on the **`event_rule`**, so an alarm moved its owner's health directly. Impact now lives on the
  **role**, and an alarm reaches a system **only** through the capabilities it degrades. An alarm on a
  component that fills no role moves that component's own verdict and nothing above it, which is the correct
  answer and was previously an accident of tagging. (c) **`health_role`**: the `required` / `redundant` /
  `informational` member tag on a `system_template_member` is superseded by **quorum plus impact** on a
  `system_role`, which expresses the same three cases without a fourth vocabulary (required is quorum 1 with
  impact `outage`, redundant is a quorum below the number assigned, informational is impact `none`). It also
  closes [ADR-0049](#adr-0049-the-system-role-capability-gated-staffing-and-the-resolved-capability-set)'s
  deferral of `impact` and its "quorum ships without health" note.
- **Tracked under** epic [#266](https://github.com/hyperscaleav/omniglass/issues/266). This is **PR8** of the
  estate-model shift toward property / event / command plus vendor / product / driver / capability / standard /
  role / health, and the slice that **closes the epic**: it is the one that consumes what the previous seven
  built.
- **Amended by [ADR-0087](#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability):**
  chain item 1 above (capability as the routing key: an alarm names the capabilities it degrades, a component
  satisfies a role only when it provides every required one) retires with the whole capability registry
  ([#626](https://github.com/hyperscaleav/omniglass/issues/626)). An alarm now impairs its component's own
  verdict wholesale, and a role's occupant satisfies it whenever that verdict is not `outage`. Items 2 through
  5 (the pure judgement package, the transition-only record on `state_datapoint`, recompute-at-the-write, and a
  report computing what it serves) are unchanged.
