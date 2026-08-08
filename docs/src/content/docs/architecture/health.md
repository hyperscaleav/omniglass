---
title: Health, KPIs, and service levels
description: "Health as a verdict rolled up from alarms through occupied slots and roles, recorded as a transition so the edges are accurate, plus the KPIs every estate should track and SLI / SLO / SLA."
sidebar:
  badge:
    text: Partial
    variant: note
---

Health answers "is this system working right now?" and "since when?". It is a **first-class
model**, not a rules-engine byproduct: a deliberate chain (an alarm impairs a component's own
verdict, a down component no longer occupies the role it fills, an impaired role sinks its system
by a declared impact) carried on the ordinary sample pipeline: stored, queried, and trended like any
other signal.

:::note[Partial]
Built today: the **`alarm`** table (component-local, impairing its component wholesale), **`impact`**
on a `system_role`, the **rollup** from component through system to location, and the **recorded
transition history**. Two reads serve it (`GET /systems/{name}/health` and
`GET /locations/{name}/health`) alongside the alarm write surface on a component. The console
shipped **HealthPanel**, **HealthBadge**, **HealthHistory**, and **AlarmsPanel** on the component,
system, and location details. See [implementation status](/architecture/status/).
:::

## The chain: a component's own verdict is the routing key

Health is a chain, every hop a thing an operator already models:

```text
alarm on a component
  -> the component's own verdict goes down (any active alarm; #626)
    -> it no longer occupies the roles it is assigned to
      -> a role falls below its quorum and is impaired
        -> the role contributes its declared impact
          -> the system takes the worst contribution
            -> the location takes the worst of its systems
```

- An **[alarm](#the-alarm-what-is-wrong-with-one-component)** is **component-local** and impairs its
  component **wholesale**: any active alarm degrades it, a critical one is an outage. It no longer
  names what it takes away (#626 retired the capability registry that used to route it).
- A component **occupies** a [system role](/architecture/core-entities/#system-roles-the-slots-a-system-needs-filled)
  it is assigned to only when its own verdict is currently **healthy**. The typed-slot guard
  (accepted `component_type`s, optionally pinned products) is checked once, at assignment; it plays
  no further part in health.
- A role with fewer occupying components than its **quorum** is **impaired**, contributing its
  **impact**; the system takes the **worst** contribution, the location the worst across its
  subtree's systems.

A component's own verdict is the only thing that travels from the box that breaks to the slot that
cares: hence the routing key ([ADR-0050](/architecture/decisions/#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain), amended #626).

## Impact lives on the role

**`impact`** is a column on `system_role`: `outage`, `degraded`, or `none`, defaulting to `degraded`.
It lives on the **role**, not the alarm or component, because the same broken box matters
differently per slot: a dead confidence monitor is not a dead main display.

| impact | an impaired role means | use it for |
|---|---|---|
| `outage` | the system is not working | the slot the room cannot run without |
| `degraded` | the system is working, worse | the slot that costs quality, not the meeting |
| `none` | nothing | a slot you track but do not depend on |

**Quorum is the redundancy knob**: 1 with two assigned tolerates one failure; 2 with two assigned
is impaired the moment either degrades. Redundancy is the gap between staffed and needed, no
separate vocabulary.

## The verdict vocabulary

A verdict is one of three values, ordered so "worst" has a meaning:

```text
healthy  <  degraded  <  outage
```

**`outage`, not `down`**: a device is down, a room has an outage, the reasoning that once picked
`ok` over `up` ([ADR-0003](/architecture/decisions/#adr-0003-health-reads-ok-not-up)).

Health is **distinct from severity**: severity is an alarm's alert importance
([alarms and actions](/architecture/alarms-actions/)), health an entity's operational state; a
`critical` alarm on a component filling no role moves nothing above it.

## The rollup is a pure function

The judgement lives in a **pure package** (`internal/health`) with **no database access**: storage
resolves the inputs and records the answer, the package decides, the subtle cases pin down in
unit tests.

Two defaults are deliberate safety calls pointing in **opposite** directions:

- An **unrecognized impact reads `degraded`**, never `healthy`: a bad value must not make an impaired
  role silently harmless.
- An **unrecognized recorded value reads `healthy`**: one stray row must not paint an estate broken.

The rule behind both: **fail loud about a judgement, fail quiet about a record.** Two more defaults
follow: a **system with no roles is `healthy`** (nothing claimed about it), and a **quorum below
one reads as one** (a role no component need fill is not a role).

## Health is recorded as a transition

> The most important thing about health is that we have a real, accurate history of the edges. We need to
> know exactly when a system went from healthy to unhealthy, and be able to look back at it weeks later.

If the history must be **accurate**, the verdict must be computed at the **write** that changed it;
if it must be **edges**, the right carrier already exists. Health lands in
the **`property` lane**, **already transition-only** (the ingest path writes a row only when the value
differs from the last stored), reusing that primitive with its own owner-arc read
(`healthTransitions`, the ordered flip sequence on the
**[owner arc](/architecture/core-entities/#ownership-the-exclusive-arc)** rather than the
component-and-instance one `PropertyTransitions` reads): a component, system, or location owns its own
health series, with `provenance='calculated'` and `source_rule='health-rollup'` naming the producer.
There is **no `health_history` table**: it would be a second, worse copy of one that already
exists.

An owner's first value is **always** recorded, even `healthy`, distinguishing "healthy since we
started watching" from "never evaluated".

### Two alternatives, and why both fail

**Compute the verdict on read** keeps **no history at all**, the opposite of the requirement.
**Compute on read and write the result through** is more dangerous: the history **looks** real but
is sampled by whoever opens a page (a room that broke Friday night and was opened Monday reads as
breaking Monday morning), worse than none because it will be trusted.

### Recompute at the write, in the same transaction

A verdict is recomputed by **every mutation that can change it**, inside the caller's transaction, so
the cause and the verdict commit together or not at all:

| the write | why it can move health |
|---|---|
| raise or clear an **alarm** | the component's own verdict, and every role it occupies, can flip |
| **assign** or **unassign** a component | a role reaches or falls below its quorum |
| **declare** or **withdraw** a role | a system gains or loses a slot it can be short of |
| change a role's **quorum** or **impact** | the same staffing crosses a different line |
| **create** a system | its opening verdict gives its history a beginning |
| change the **standard** a system conforms to | the whole inherited role set is swapped |
| change a system's **location** | the contribution moves between rollups, so **both** are recomputed |
| **delete** a system | the location it sat in loses a contributor and may have just improved |

A standard change moves **every conforming system** at once. The relocation case names the location
the system **left** explicitly, because its rollup may have just **improved** (a recovery is an edge
as real as a failure; **deleting** a system is the same shape). A component's **product** (and so its
`component_type`) governs the typed-slot guard checked once at **assignment**; changing it after the
fact does not, by itself, move any assigned role's health (#626: the guard is not part of the health
chain, so this row that used to reach the whole estate on a catalog edit is retired along with it).
Deleting a **location** needs no trigger: a location holding anything cannot be deleted (`on delete
restrict` throughout), so a deletable location is empty and already healthy.

A **missing trigger is a hole in the history**: the honest cost of this design, and why the list is
enumerated, not inferred.

### A read never writes

Self-healing on read would stamp the edge at read time, precisely the inaccuracy this model avoids.
The reads do, however, **compute the verdict they serve from the same rows they display**, a
correctness fix: serving the **last recorded** verdict while resolving contributing roles
**live** once let a system report `healthy` beside an impaired `outage` role. **Recorded transitions
remain the source for history**; a missing trigger can cost an **edge**, never a report that
**lies about the present**.

## Reading health

Two reads, both scope-injected, both a non-disclosing 404 for an owner outside the caller's scope
([API](/architecture/api/#health-the-verdict-and-why)).

**A system's report** is the verdict, every role it needs filled, and for an impaired role the
causing chain: "the `room-mic` role wants 2 and has 1, because `mic-pod-2` went down on a critical
alarm raised at 14:02" tells the operator where to walk. A role impaired with **no assigned component
down** (nobody assigned, or fewer assigned than the quorum wants) names no alarm, distinguishing
**short-staffed** from **broken**.

**A location's report** is the verdict plus every system beneath it with its own verdict, a map
rather than a duplicated explanation.

Both reports carry the **recorded transitions** over the last 30 days, oldest first, one entry per
change, never a sample: the availability strip's data and the answer to "since when".

## The alarm: what is wrong with one component

An alarm is a row on a component with a **`severity`** (`info`, `warning`, or `critical`), a
**message**, a **`raised_at`**, and a **nullable `cleared_at`**. Clearing sets `cleared_at` and
**keeps the row**; clearing an already-cleared alarm is an explicit miss, not a silent success.

Severity drives the **component's own** verdict (any active alarm makes it `degraded`, a `critical`
one an `outage`) and **nothing above it**: what reaches a role is whether its occupant's verdict is
`healthy` or not, not the severity that produced it. Severity is how loudly to page somebody, impact
is what the room lost. A component's verdict records on its own arc, so a component filling no role
still carries accurate history.

::::design[Target design (ADR-0050)]

## Where alarms come from

Today an **operator or API caller** writes the alarm. The full model: an
[`event_rule`](/architecture/alarms-actions/) watches samples, fires an event, and an alarm **opens**
and stays open while its condition holds, closing on the paired clear event. Health is
**ack-independent**: acking annotates, never closes, so a broken room never looks healthy. A
rule-opened alarm impairs its owner's own verdict exactly as a hand-raised one does (#626: it no
longer names what it takes away; the component is impaired wholesale).

### Alarms owned by a system or a location

The alarm arc is **component-only** today; the design gives an alarm the same
[exclusive-arc owner](/architecture/core-entities/#ownership-the-exclusive-arc) every sample and
event has, so a **system-scoped** rule reading member data raises a **system-owned** alarm for a
condition only the system cares about (a display on **input 2** is normal for the display, wrong for
the room); the component stays generic. **SaaS and vendor status**
([shared-API collection](/architecture/collection/)) follows the same discipline: a vendor's
"offline" is an observed signal to author system conditions over, **corroborated**, never a verdict.
The acyclic discipline holds: an alarm that **feeds** health impairs a component's own verdict, the
alarm that fires **off** the `health` state impairs nothing; health rolls up only, no loop.

### `unknown`, and honest coverage

The built domain has three values and no `unknown`. The design adds a fourth reading, **off the
order**: covered, silent, and fresh reads `healthy`; **not covered** reads **`unknown`** (a
`healthy` there would be a false green), with a **reason** discriminator: **`stale`** (the no-data
machinery in [time](/architecture/time/)), **`uncovered`** (no health-impacting rule resolves),
**`no-data`** (covered, never reported). Every **collected component** seeds a baseline reachability
alarm, keeping `uncovered` rare.

:::caution[Open question]
How `unknown` composes upward. A required role whose only component is unmeasured is not `healthy`,
but calling the system an outage overstates it.
:::

### The `global` estate top

The rollup ends at a location today; the design adds the singleton **`global`** owner above every
location for the estate-wide verdict and KPIs. The **owner** gives a reading its level: one `health`
key serves component, system, location, and global without cross-triggering.

::::

::::design[The SLI / SLO / SLA and KPI tier (ADR-0050)]

## SLI: indicator over a window

A **Service Level Indicator** is a `time_in_state` calc over a window, derived from the recorded
health transitions and emitted as its own property (the temporal reducer,
[expressions](/architecture/expressions/)):

```yaml
# availability = fraction of the last 30 days the system was healthy
source: { property: health, over: 30d }
reduce: time_in_state
when: "value.healthy / value.total"   # an Expr leaf shapes it into a ratio
# -> emits system-availability
```

An SLI is just another derived property, and transition-only recording's clearest payoff:
`time_in_state` over a stream of edges is exact and cheap, over samples an approximation.

## SLO and SLA: the target, and meeting it

The **SLI** is the *measured indicator* (the `system-availability` calc above); the **SLO** is the
**target** (availability >= 99.9%), a [config](/architecture/variables/) value on the entity or
standard; the **SLA** is **meeting the SLO**: an `event_rule` fires when the SLI breaches the
target, and compliance over the contractual window is itself an SLI.

```yaml
event_rule:
  scope: 'system.standard == "meeting-room"'
  property: system-availability
  when: "value < $var:availability-slo"   # the SLO target, a config value
  severity: high
```

Windowing is the SLI's concern: a **rolling** window (last 30d) for trends, a **calendar** window
(the billing month) for a contractual SLA, the calendar reset leaning on the time primitive.

:::caution[Open question]
The SLA calendar-window boundaries and timezone, co-designed with the time primitive.
:::

## KPIs: what every estate should track

A **KPI** is a derived property (a calc or SLI), registered as canonical and owned at the level it
describes (system, location, or **global**). Omniglass ships an opinionated **default set**, plus
the escape hatch to author your own. **Availability** is health over time, the SLI
`time_in_state(healthy)` above, shipped free at every level up to global.

**Utilization** is the AV-native family, over occupancy and booking data:

- **occupancy**: current people / capacity (an instant ratio);
- **time-utilization**: used vs idle minutes;
- **booking-utilization**: booked vs unbooked minutes;
- **ghost**: occupied vs booked, so booked but nobody showed (the wasted-room signal).

Both inputs are **ordinary components**: an occupancy sensor emitting the `occupancy-*` family, and the booking
system as a component whose interface is the calendar API, emitting `booking-*`; the KPIs are calcs
over those samples, owned at room / system / location / global (a ghost meeting is
`occupied < booked`).

:::caution[Open question]
The full default KPI set and each one's exact reducers and windows.
:::

:::caution[Open question]
The `occupancy-*` and `booking-*` canonical signals, and the occupancy-sensor and booking-system
component templates that feed the utilization KPIs.
:::

::::

## Why this is the Zabbix service tree, done right

Zabbix bolts services, SLA, and the service tree on as a separate subsystem. Omniglass makes health
**first-class but not separate**: the **system tree is the service tree**, the verdict a `property`
sample, the history its transitions, the SLI a calc over them, the SLA an alarm.

Related: [core entities](/architecture/core-entities/#system-roles-the-slots-a-system-needs-filled)
(the role, the typed-slot guard, the quorum), [alarms and actions](/architecture/alarms-actions/) (the
detection tier), [samples](/architecture/properties/) (the `property` lane and the owner arc), and
the [Standards](/guides/admin/standards/) and [Work with an entity](/guides/operator/entities/)
guides for the operator loop.
