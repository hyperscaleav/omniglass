---
title: Health, KPIs, and service levels
description: "Health as a first-class state rolled up to a global estate top, the KPIs every estate should track (availability, utilization), and SLI / SLO / SLA."
sidebar:
  badge:
    text: Design
    variant: caution
---

Health gives an operator the one answer that matters most, "is this system working right now?", as a first-class state on every entity that rolls up the service tree, not something you have to assemble out of raw rules. Omniglass is **opinionated about health**: it is a
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

- **Intrinsic.** Every component, system, and location *has* health, automatically, moved by its
  open health-impacting alarms when it is covered and `unknown` until then (below). No entity is
  health-less, and none waits on someone authoring a rule.
- **A built-in ordered domain** (below), not a user-defined value type.
- **Alarm-sourced.** Health is computed from open **alarms**, not measured or extracted (below).
- **A built-in role-aware rollup** up the structural tree (below): engine behavior, not an editable
  reducer.

What is **reused from the carrier**: storage, history, `current_value` projection, the SLI
(`time_in_state` over health history), alarming (an `event_rule` on `health`), and backtest. An
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

- A **component's health** is the **worst** over its **open health-impacting alarms**. Reachability
  is just one such alarm (an "unreachable" trigger, impact `down`): everything is an alarm, with no
  parallel datapoint-calc.
- Health is **ack-independent**, because ack is not close. An alarm stays open (acknowledged) while
  its condition holds; only the **clear event** (the data recovered) closes it. Acking annotates; it
  never makes a down room look healthy.

So "twelve alarms open, system fine" falls out for free: if none of the twelve is health-impacting,
health never moved.

### No firing alarm splits by coverage

When **no** health-impacting alarm is open, the honest answer depends on whether anything would have
caught a failure. **Coverage** is the question "does any health-impacting `event_rule` resolve
against this entity's datapoint_types?":

- **covered, none firing, data fresh -> `up`.** Something measures what "down" means here, it is
  watching, and it is silent: genuinely healthy.
- **not covered -> `unknown`.** No health-impacting rule resolves, so nothing here knows what failure
  looks like. Reporting `up` would be a false green; the entity is `unknown`, not healthy.

`unknown` carries a **reason** discriminator as metadata, so an operator can tell a measurement gap
from a coverage gap. The ordered value domain (below) is **unchanged**: `unknown` stays off the
order, and the reason is descriptive metadata, not a new state. The reasons:

- **`stale`** -- the entity had data and it went stale (the no-data machinery in
  [time](/architecture/time/)).
- **`uncovered`** -- no health-impacting rule resolves against its datapoint_types (this concern).
- **`no-data`** -- a rule covers it, but it has never reported, so the rule has nothing to evaluate.

To keep `uncovered` the rare, honest resting state rather than the default, every **collected
component** is **seeded with a baseline reachability health-impacting alarm** (an "unreachable"
trigger, impact `down`, via the collection / template default). A freshly-collected device is
therefore covered the moment it is collected, and resolves `unknown -> up` or `unknown -> down` on
its first poll. Bare `unknown(uncovered)` then means exactly one thing: "you have collected this, but
you have not told me what failure looks like beyond reachability," a deliberate gap to fill, not a
silent hole.

## The health-state vocabulary

Health is a **state** with a small **fixed ordered** value domain, declared as its datapoint_type:

```text
up  <  degraded  <  down            unknown = no signal (not on the order)
```

It is **distinct from severity**. Severity is alert importance (a named level by id,
[alarms and actions](/architecture/alarms-actions/)); health is entity operational state. They map
(a `down` health typically raises a `high` or `disaster` alarm) but they are different axes, so health
is not a severity level. The order exists so the `worst` reducer can pick the worst member;
`unknown` is off the order (carrying a reason of `stale`, `uncovered`, or `no-data`, above) and is
surfaced, not silently folded as `down`.

## Rollup up the structural tree

Health composes recursively, always **up the structural tree** (which has no cycles):

```text
component health   (worst over the component's own open health-impacting alarms)
   -> system health      (role-aware rollup of members + the system's own health-impacting alarms)
      -> location health  (role-aware rollup of its systems)
         -> global health  (rollup of every location: the estate top)
```

The headline is the **system's `health`**, the rollup of its members, the service view operators
care about; the **`global`** rollup is the estate-wide view leadership cares about.

**One owner-agnostic `health` key.** Following the measurement-not-owner naming model
([datapoints](/architecture/datapoints/)), there is a single registered `health` datapoint_type; a
component owns its own `health`, a system the rollup of its members, a location the rollup of its
systems, and the singleton **`global`** owner the estate-wide rollup (the top of the tree, above
every location). The owner gives the reading its level, so the same key flows up the tree. The calc
engine routes a changed `health` by the owner's level against each rule's source (a component's
`health` only feeds the system rollup, a system's only the location, a location's only the global),
so the shared key never cross-triggers.

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

The same discipline governs **SaaS and vendor status** (a UCC platform like Zoom, mapped to
system-owned datapoints, [shared-API collection](/architecture/collection/)): a vendor's reported
"offline" or "in a meeting" is an *observed signal from one source*, not a verdict on the room. Author
the system condition over it, **corroborated** where you can (against the codec, occupancy), rather
than trusting it. The vendor's opinion is an input to health, not health itself, the same way no
single component's state is.

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
- **required** member `unknown` -> system `unknown` (the system cannot be called healthy when a
  member it depends on is unmeasured);
- **redundant** member `down` -> system `degraded` (only `down` if *all* redundant peers are down);
- **informational** member -> does not affect system health, including an `informational` member that
  is `unknown` (an unmeasured member that never mattered does not sink the parent).

:::caution[Open question]
The exact `redundant`-group semantics when a system has several independent redundant sets (per-set
quorum versus one pool), and whether `degraded` is one rung or graduated.
:::

A redundant deployment needs exactly this: a failed backup mic must not down the room. **Member
`health_role`** (required / redundant / informational) is declared on the **system_template_member**
(the frozen BOM, where the system template composes component-template versions by role), not on the
component itself, since the same device can be required in one system and redundant in another. The
instance assignment is the `system_member` row; the `health_role` rides the frozen template version,
so it never expires under an instance. It is shared with KPI calcs.

For the rare case the role logic still gets wrong, an **Expr override at the system-template level**
over the member health states is the escape hatch, reached for rarely.

:::caution[Open question]
Whether a system-template binding may narrow the built-in rollup (a scoped-precedence refinement),
or only the roles and the Expr override are the knobs.
:::

The rollup **runs over the calc engine** (no parallel evaluator) and is **seeded for every system,
location, and the global top**, so health rolls up out of the box without per-system authoring:
`system-health` reduces a system's members' `health`, `location-health` a location's systems',
`global-health` every location's into the estate top (each treated as required above the system
level: any down child sinks the parent). It is the model's behavior, not a rule operators rewrite.

:::caution[Open question]
A single `required` `unknown` member already makes the system `unknown` (above). The remaining
question is the all-`unknown` system with no forcing required member (every member `unknown`, or only
`informational` ones reporting): gray, or the parent's prior state. The `unknown` versus `stale`
distinction itself is settled in [time](/architecture/time/).
:::

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

## SLO and SLA: the target, and meeting it

Three terms, not two. The **SLI** is the *measured indicator* (the `system.availability` calc above).
The **SLO** (Service Level Objective) is the **target**: the number you intend to hold
(availability >= 99.9%), a [config](/architecture/variables/) value on the entity or template, not
machinery. The **SLA** (Service Level Agreement) is **meeting the SLO**: an `event_rule` fires when
the SLI breaches the target, and compliance over the contractual window (the fraction of the period
the SLO held) is itself an SLI.

```yaml
event_rule:
  scope: 'system.template == "standard-boardroom"'
  datapoint: system.availability
  when: "value < $var:availability.slo"   # the SLO target, a config value
  severity: high
```

So the target is config (the SLO), the breach is an event/alarm (the SLA edge), and compliance is a
calc (an SLI over the SLA). No new machinery. Windowing is the SLI's concern: a **rolling** window
(last 30d) for trends, or a **calendar** window (the billing month) for a contractual SLA; the
calendar reset is the one piece that leans on the time primitive.

:::caution[Open question]
The SLA calendar-window boundaries and timezone, co-designed with the time primitive.
:::

## KPIs: what every estate should track

A **KPI** is a derived datapoint (a calc or SLI), registered as a canonical `datapoint_type` and
owned at the level it describes (system, location, or **global**). It is no new primitive: a KPI is a
shipped calc the same way health is. Omniglass ships an opinionated **default set** so the data is
there out of the box, with the escape hatch to author your own.

**Availability** is health over time: the SLI `time_in_state(up)` above. Health is the substance,
availability is its ratio, so it ships free at every level up to global.

**Utilization** is the AV-native family, over occupancy and booking data:

- **occupancy** -- current people / capacity (an instant ratio);
- **time-utilization** -- used vs idle minutes;
- **booking-utilization** -- booked vs unbooked minutes;
- **ghost** -- occupied vs booked: booked, but nobody showed (the wasted-room signal).

Both inputs are **ordinary components**, no special integration: an occupancy sensor (a component
template emitting `occupancy.*`) and the booking system (a component template whose interface is the
calendar / room-booking API, emitting `booking.*`). The KPIs are then `calc_rule`s over those
datapoints, owned at room / system / location / global like any rollup. A booking API is just an
interface; a ghost meeting is just `occupied < booked`.

The point is a small, opinionated set of the measurements every estate should watch, computed and
rolled up for free.

:::caution[Open question]
The full default KPI set and each one's exact calc. Availability and the utilization family are
named, but the precise reducers and windows are unsettled.
:::

:::caution[Open question]
The `occupancy.*` and `booking.*` canonical signals, and the occupancy-sensor and booking-system
component templates that feed the utilization KPIs.
:::

## Why this is the Zabbix service tree, done right

Zabbix bolts services, SLA, and the service tree on as a separate subsystem. Omniglass does the
opposite: health is **first-class but not separate**. The model is opinionated (an intrinsic state,
health-impacting alarms, a role-aware rollup) and it rides the one datapoint pipeline, so the
**system tree is the service tree**: health is a datapoint, the rollup is built-in, the SLI is a
calc, the SLA is an alarm. One model, composed, instead of a parallel feature. An operator who
understands datapoints and alarms already understands health and SLAs.

