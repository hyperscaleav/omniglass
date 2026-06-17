---
title: Health, SLI, and SLA
description: "The first-class health model: an opinionated, role-aware operational-state rollup carried on the datapoint pipeline, with SLI and SLA on top."
---

Leaf of the [architecture spine](/architecture/). Omniglass is **opinionated about health**: it is a
**first-class capability**, not a byproduct of a customizable rules engine. The *model* is
deliberate (an ordered state, a health impact on alarms, a role-aware rollup up the system tree);
the *carrier* is the ordinary datapoint pipeline, so health is stored, queried, trended, and alarmed
on like any other signal, with no parallel subsystem.

## Health is a first-class model, carried as a datapoint

`health` is a built-in **state of every structural entity**, not a datapoint type an operator
happens to author. But its **representation** is an ordinary derived `state_datapoint`
(provenance=calculated), so it inherits the whole pipeline for free: stored, queried, projected to
current-value, trended, and able to raise alarms. The model is opinionated; the carrier is reused.
There is **no separate health store, no `health_event`, no parallel service subsystem**, because
pulling health off the datapoint stream is exactly the Zabbix services bolt-on this design rejects.

What is **first-class about the model** (not ordinary):

- **Intrinsic.** Every component, system, and location *has* health, automatically: `up` by
  default, moved only by its open health-impacting alarms. No entity is health-less, and none waits
  on someone authoring a rule.
- **A built-in ordered domain** (below), not a user-defined value type.
- **Alarm-sourced.** Health is computed from open **alarms**, not measured or extracted (below).
- **A built-in role-aware rollup** up the structural tree (below): engine behavior, not an editable
  reducer.

What is **reused from the carrier**: storage, history, `current_value` projection, the SLI
(`time_in_state` over health history), alarming (an `event_rule` on `health`), and replay. An
operator who understands datapoints and alarms already understands health.

## Health is built from alarms

Health is a **state**, and a state must be built from something stateful. An **event is a stateless
edge**: it just happens, so health cannot hang off events. It hangs off the **alarm**, the stateful
PROBLEM that holds open as long as its condition does (the Zabbix-trigger model). The chain:

> datapoints -> an **`event_rule`** decides "something we care about" -> an **alarm** opens -> the
> alarm *optionally* carries a **health impact**.

An `event_rule` (the alarm's definition) declares an optional **`health` impact**: `down`,
`degraded`, or none (the default). Most alarms carry none, because a lamp-hours warning is worth an
alert but does not down the device; the few that do are the device's actual failure conditions.

- A **component's health** is the **worst** over its **open health-impacting alarms** (and `up` if
  none). Reachability is just one such alarm (an "unreachable" trigger, impact `down`): everything
  is an alarm, with no parallel datapoint-calc.
- Health is **ack-independent**, because ack is not close. An alarm stays open (acknowledged) while
  its condition holds; only the **clear event** (the data recovered) closes it. Acking annotates; it
  never makes a down room look healthy.

So "twelve alarms open, system fine" falls out for free: if none of the twelve is health-impacting,
health never moved.

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
component health   (worst over the component's own open health-impacting alarms)
   -> system health      (role-aware rollup of members + the system's own health-impacting alarms)
      -> location health  (role-aware rollup of its systems)
```

The headline is the **system's `health`**, the rollup of its members, which is the service view
operators care about.

**One owner-agnostic `health` key.** Following the measurement-not-owner naming model
([taxonomy](/architecture/taxonomy/)), there is a single registered `health` datapoint_type; a
component owns its own `health`, a system owns the rollup of its members, a location owns the rollup
of its systems. The owner gives the reading its level, so the same key flows up the tree. The calc
engine routes a changed `health` by the owner's level against each rule's source (a component's
`health` only feeds the system rollup, a system's only feeds the location rollup), so the shared key
never cross-triggers.

## System health: rollup plus the system's own alarms

A system's health is the **worst of two inputs**:

1. the **role-aware rollup** of its members' health (below), and
2. the system's **own open health-impacting alarms**, raised by **system-scoped `event_rule`s** over
   member data.

The second input is what lets a system see what no single component can. The canonical case: a
display sitting on **input 2** is a perfectly normal state *for the display* (no alarm), but in a
specific room it means the wrong source is on screen. A system-scoped event_rule ("this display must
be on input 1") opens a **system-level alarm** with a health impact, dropping system health while the
display's own health stays `up`. The system template owns the conditions only the system cares about;
the component stays generic.

This is the symmetry: **component-level events and alarms** and **system-level events and alarms**,
the same machinery on each arc, distinguished by which entity owns the arc (the exclusive-arc owner,
[alarms and actions](/architecture/alarms-actions/)).

The acyclic discipline: an alarm that *feeds* health is impact-tagged; the "system is down" alarm
that fires *off* health (an `event_rule` watching the `health` datapoint) carries no impact. Inputs
are tagged, consequences are not, and health rolls up only, so there is no loop.

## Role-aware rollup: built-in, tuned by role

The rollup is **engine behavior, not an editable rule**. Health is opinionated, so the reducer is
built in and the same everywhere; what an operator tunes is **roles and thresholds**, never the
reducer's guts. Each member carries a **`health_role`**, and the rollup respects it:

- **required** member `down` -> system `down`;
- **redundant** member `down` -> system `degraded` (only `down` if *all* redundant peers are down);
- **informational** member -> does not affect system health.

A redundant deployment needs exactly this: a failed backup mic must not down the room. **Member
`health_role`** (required / redundant / informational) is declared on the **system_template_member**
(the frozen BOM, where the system template composes component-template versions by role), not on the
component itself, since the same device can be required in one system and redundant in another. The
instance assignment is the `system_member` row; the `health_role` rides the frozen template version,
so it never expires under an instance. It is shared with KPI calcs.

For the rare case the role logic still gets wrong, an **Expr override at the system-template level**
over the member health states is the escape hatch, reached for rarely.

The rollup **runs over the calc engine** (no parallel evaluator) and is **seeded for every system
and location**, so health rolls up out of the box without per-system authoring: `system-health`
reduces a system's members' `health`, `location-health` reduces a location's systems' `health`
(systems carry no `health_role` within a location, so each is treated as required: any system down
sinks the location). It is the model's behavior, not a rule operators rewrite.

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

Zabbix bolts services, SLA, and the service tree on as a separate subsystem. Omniglass does the
opposite: health is **first-class but not separate**. The model is opinionated (an intrinsic state,
health-impacting alarms, a role-aware rollup) and it rides the one datapoint pipeline, so the
**system tree is the service tree**: health is a datapoint, the rollup is built-in, the SLI is a
calc, the SLA is an alarm. One model, composed, instead of a parallel feature. An operator who
understands datapoints and alarms already understands health and SLAs.

## Open items

- The rollup's treatment of an all-`unknown` system (gray versus the parent's prior state). The
  `unknown` versus `stale` distinction itself is settled in [time](/architecture/time/).
- The exact `redundant`-group semantics when a system has several independent redundant sets
  (per-set quorum versus one pool), and whether `degraded` is one rung or graduated.
- Whether a system-template binding may narrow the built-in rollup (a scoped-precedence refinement)
  or only the roles and the Expr override are the knobs.
- SLA calendar-window boundaries and timezone (co-design with the time primitive).
