---
title: Health, SLI, and SLA
description: "The operational-state rollup and the service-level layer, introduced as no new primitive: calc, reducers, the system tree, and alarms, composed."
---

Leaf of the [architecture spine](/architecture/). The operational-state rollup and the
service-level layer on top of it. It introduces **no new primitive**; it is entirely calc,
reducers, the system tree, and alarms, composed.

## Health is a state datapoint

`health` is an **ordinary derived state datapoint**: a `state_datapoint` (provenance=calculated)
written by a `calc_rule` that reduces over inputs. There is **no `health_rule`, no `health_event`,
and no `component_health` primitive**. Health is just a datapoint computed rather than measured, so
it inherits the whole pipeline for free: it is stored, queried, projected to current-value, and can
itself raise alarms.

**One owner-agnostic `health` key.** Following the measurement-not-owner naming model
([taxonomy](/architecture/taxonomy/)), there is a single registered `health` datapoint_type; a
component owns its own `health`, a system owns the rollup of its members, a location owns the rollup
of its systems. The owner gives the reading its level, so the same key flows up the tree. The calc
engine routes a changed `health` by the owner's level against each rule's source (a component's
`health` only feeds the system rollup, a system's only feeds the location rollup), so the shared
key never cross-triggers.

## The health-state vocabulary

Health is a **state** with a small **fixed ordered** value domain, declared as its datapoint_type:

```text
up  <  degraded  <  down            unknown = no signal (not on the order)
```

It is **distinct from severity**. Severity is alert importance (an open integer plus lookup,
[alarms and actions](/architecture/alarms-actions/)); health is entity operational state. They map
(a `down` health typically raises a high or disaster alarm) but they are different axes, so health
is not the severity integer. The order exists so the `worst` reducer can pick the worst member;
`unknown` is off the order (stale or no data, ties to the no-data machinery in
[time](/architecture/time/)) and is surfaced, not silently folded as `down`.

## Rollup up the structural tree

Health composes recursively, always **up the structural tree** (which has no cycles):

```text
component health   (optional leaf: a template-defined calc over the component's
                    own datapoints, e.g. reachable + key error states)
   -> system health      (a calc reducing its members' health)
      -> location health  (a calc reducing its systems' health)
```

A component's `health` is **optional and template-defined** (the device shape knows what "healthy"
means for that device class; if absent, a member contributes a default derived from reachability).
The headline is the **system's `health`**, the rollup of its members, which is the service view
operators care about.

## Default, role-aware, and override

Three tiers, from trivial to escape-hatch:

- **default**: `health = worst(member health)`. Role-blind; correct for simple systems where any
  member down means the system is down.
- **role-aware (the recommended default)**: each member carries a **`health_role`**, and the rollup
  respects it:
  - **required** member `down` -> system `down`;
  - **redundant** member `down` -> system `degraded` (only `down` if *all* redundant peers are
    down);
  - **informational** member -> does not affect system health.
  This is what a redundant deployment needs (a failed backup mic must not down the room).
- **override**: an **Expr expression at the system-template level** over the member health states,
  for the cases the role logic still gets wrong. The escape hatch, reached for rarely.

**Member `health_role`** (required / redundant / informational) is declared on the
**system_template_member** (the frozen BOM, where the system template composes component-template
versions by role), not on the component itself, since the same device can be required in one system
and redundant in another. The instance assignment is the `system_member` row; the `health_role`
rides the frozen template version, so it never expires under an instance. It is shared with KPI
calcs.

## Shipped defaults

The role-aware rollups ship as **seeded official `calc_rule`s** (no parallel evaluator, just specs
over the calc engine), so health rolls up out of the box without per-system authoring:

- **`system-health`**: `health_by_role` over a system's members' `health` -> the system's `health`.
- **`location-health`**: `health_by_role` over a location's systems' `health` -> the location's
  `health`. Systems carry no `health_role` within a location, so each is treated as required: any
  system down sinks the location (worst-health semantics).

Both have an **empty scope**, so they fire for every system and location. The leaf, a component's
own `health`, is deliberately **not** seeded as a calc rule: it is device-specific and belongs in
component-template authoring (a `self` cross-key calc), not a universal default. Until it is
authored, a system with no member `health` inputs folds to `up` (no signal is read as healthy).

To change the rollup, operators **edit the seeded rule itself**: re-POST `/rules/calc` with the
same id and a new spec, which appends a new version (content-idempotent, so a no-op until the spec
actually differs). The engine does **not** rank rules by scope: authoring a *second* rule that emits
the same output key for an overlapping instance set is a write-write collision (both fire, last
write by timestamp wins), not an override. Per-template variation therefore means editing the
default's scope and spec, not layering a narrower rule on top. A scope-precedence model (template
binding beats empty scope) is a possible later refinement.

## SLI: indicator over a window

A **Service Level Indicator** is a `time_in_state` calc over a window, emitted as its own datapoint
(the temporal reducer, [expressions](/architecture/expressions/)):

```yaml
# availability = fraction of the last 30 days the system was up
source: { datapoint: health, over: 30d }
reduce: time_in_state
when: "value.up / value.total"        # an Expr leaf shapes it into a ratio
# -> emits system.availability
```

An SLI is therefore just another derived datapoint, queryable and trendable like any other.

## SLA: a target plus a breach alarm

A **Service Level Agreement** is a **target on an SLI plus an alarm on breach**, no new machinery:
an `event_rule` whose datapoint is the SLI:

```yaml
event_rule:
  scope: 'system.template == "standard-boardroom"'
  datapoint: system.availability
  when: "value < 0.999"               # 99.9% target
  severity: 40
```

Windowing is the SLI's concern: a **rolling** window (last 30d) for trends, or a **calendar** window
(the billing month) for a contractual SLA. The calendar-window reset is the one piece that leans on
the time primitive.

## Why this is the Zabbix service tree, done right

Zabbix bolts services, SLA, and the service tree on as a separate subsystem. Here the **system tree
is the service tree**: health is a datapoint, the rollup is a calc, the SLI is a calc, and the SLA
is an alarm. One model, composed, instead of a parallel feature. An operator who understands
datapoints and alarms already understands health and SLAs.

## Open items

- The rollup's treatment of an all-`unknown` system (gray versus the parent's prior state). The
  `unknown` versus `stale` distinction itself is settled in [time](/architecture/time/).
- Whether the role-aware rollup is a named reducer (`health_rollup`) or a small built-in expression
  template; either keeps Expr in the leaf.
- The exact `redundant`-group semantics when a system has several independent redundant sets
  (per-set quorum versus one pool).
- SLA calendar-window boundaries and timezone (co-design with the time primitive).
