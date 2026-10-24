---
title: "Health: the alarm-capability-role chain, and a verdict recorded as a transition"
date: 2026-07-21
refs: []
form: bullet
---

- **Health: the alarm-capability-role chain, and a verdict recorded as a transition.** A system and a location
  now answer "is this working right now?" and "since when?". An **`alarm`** is **component-local** (a
  `severity` of `info` / `warning` / `critical`, a `message`, a `raised_at`, and a **nullable `cleared_at`**,
  so clearing **keeps the row** and the record of what was wrong outlives the fix), and **`alarm_capability`**
  names the capabilities it **degrades**. That naming is the only route out of the component: a component
  **satisfies** a role only when it provides **every** required capability **and none of those is currently
  degraded**; a role with fewer satisfying components than its **quorum** is **impaired**; an impaired role
  contributes the new **`system_role.impact`** (`outage` / `degraded` / `none`, defaulting to `degraded`,
  landing here because this is the slice that reads it); a system takes the **worst** contribution among its
  roles and a location the worst among the systems placed anywhere beneath it. The verdict domain is
  **`healthy` < `degraded` < `outage`**, and the judgement is a **pure package** (`internal/health`) unit-tested
  with **no database**: `Satisfies`, the quorum boundary, worst-wins, and two deliberate safety defaults in
  opposite directions (an unrecognized **impact** reads `degraded`, so a bad value never makes an impaired role
  silently harmless; an unrecognized **recorded value** reads `healthy`, so one stray row cannot paint an estate
  broken). The conceptual heart is **how health is recorded**. It is written **transition-only** onto
  **`state_datapoint`**, which is already exactly that primitive (a row only when the value differs from the
  last one stored, and `StateTransitions` reads the ordered flips the reachability strip draws), on the owner
  arc with `provenance='calculated'` and `source_rule='health-rollup'`; there is **no new history table**. Two
  alternatives were rejected for failing the requirement that the **edges be accurate weeks later**:
  **compute-on-read** keeps no history at all, and **compute-and-write-through-on-read** keeps a history
  **sampled by whoever opens a page**, so the recorded edge is stamped when somebody looked rather than when the
  estate changed. Instead the verdict is **recomputed at the writes that can change it, in the same
  transaction**: alarm raise and clear, assign and unassign, role declare and withdraw, a quorum or impact
  change, a component capability or **product** change, system create (which gives a system's history a defined
  beginning), a **standard** change (which moves every conforming system), and a **relocation** (recomputing
  **both** the location arrived at and the one left, since that one may have just improved). **A read never
  writes**, and it computes the verdict it serves from the **same resolved rows it displays**, a correctness fix
  for a report that could otherwise say `healthy` beside an impaired `outage` role; recorded transitions stay
  the source for history. The slice also forced a **latent bug** into the open: recording an opening verdict at
  system creation gave every system a `state_datapoint` row from birth, and **every rename then failed on the
  owner foreign key**, because those FKs address the owner **by name** and declared no `ON UPDATE`. Migration
  `20260721170000` re-adds all four `state_datapoint` owner FKs with **`on update cascade`**, which is what
  name-as-address always meant; the same gap on `metric_datapoint`, `event`, `property_value`, `alarm`, and the
  role tables is tracked in [#314](https://github.com/hyperscaleav/omniglass/issues/314). Five routes ship,
  regenerated into the OpenAPI document, the cobra CLI, and the typed SPA client:
  `GET` / `POST /components/{name}/alarms` and `DELETE /components/{name}/alarms/{id}` (gated `component:read` /
  `:update`, an unknown capability or bad severity a 422), plus `GET /systems/{name}/health` and
  `GET /locations/{name}/health` (gated `system:read` / `location:read`, scope-injected with a non-disclosing
  404), each returning the verdict, the contributing roles with their degraded capabilities and the causing
  alarms (or the systems beneath, for a location), and the recorded transitions over the last 30 days. The CLI
  reads `omniglass component alarms|raise-alarm|clear-alarm`, `omniglass system health`, and
  `omniglass location health`, and the seed adds a **`health`** state-kind property. The console carries a
  **health badge** on the system and location details and in the systems list, a **Health** panel that names
  the causal chain role by role (alarm -> degraded capability -> role below quorum -> verdict), a **History**
  strip reusing the reachability availability-strip shape, and an **Alarms** panel on the component (raise,
  clear, and a recently-cleared group). Proven by the
  `internal/health` unit suite (the quorum boundary, a degraded assignee dropping a role below it, worst-wins at
  both levels, the impact mapping, and the verdict round-trip) and the storage and HTTP integration suites (the
  transitions written through the whole chain, an irrelevant degraded capability changing nothing, the report
  naming the cause, impact and quorum, the moves on unassign / standard change / product change / relocation,
  the fresh system's report, **health surviving a rename**, the alarm listing and its refusals, and the
  out-of-scope non-disclosing 404). This is **PR8** of the estate-model shift
  ([ADR-0050](/architecture/decisions/#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain)),
  and it **closes epic [#266](https://github.com/hyperscaleav/omniglass/issues/266)**: the slice that consumes
  what the previous seven built. [Health](/architecture/health/) advances from `Design` to **`Partial`** (alarms
  raised by an `event_rule`, system- and location-owned alarms, the `unknown` verdict, the `global` estate top,
  and the SLI / SLO / SLA and KPI tier are still `Design`); [core entities](/architecture/core-entities/) and
  [API](/architecture/api/) stay `Partial`.
