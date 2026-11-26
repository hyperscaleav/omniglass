---
title: Health
description: "How health is built: a chain from a component's alarms through the slots it fills to its system and location, decided by a pure function and recorded as transitions at the write that moved it."
sidebar:
  order: 40
  badge:
    text: Partial
    variant: note
---

**Health** is what a system, a location or a component is right now: `healthy`, `incomplete`,
`degraded` or `outage`. It is the one thing an operator reads first, and it answers two questions:
"is this working?" and "since when?". An endpoint's up or down is its **reachability**, an input to
health rather than a health of its own. To work with health in the console, see
[Work with an entity](/operator/entities/) and [alarms](/operator/alarms/); the alarms that drive it are
on [alarms and actions](/architecture/alarms-actions/).

Health is a first-class model, not a by-product of the rule engine. It is a chain where every link is
something an operator already declares, decided by a pure function, and recorded on the ordinary
sample pipeline so its history is as queryable as any other signal.

## The chain

```text
an alarm on a component
  -> the component's own health moves by the effect its severity level declares
     (none, degraded or outage)
    -> only an outage component stops filling the slots it is assigned to
      -> a slot with fewer filling components than its quorum is impaired
        -> an impaired slot contributes its impact to its system
          -> the system takes the worst contribution
            -> a location takes the worst of the systems and locations under it
```

- An [alarm](/architecture/alarms-actions/#the-alarm-a-stateful-entity) is **component-local** and
  moves its component's health **wholesale**, by the `health_effect` of its
  [severity level](/architecture/alarms-actions/#severity-a-catalog-of-named-levels): `none` leaves the
  component healthy (the alarm is still listed), `degraded` degrades it, `outage` takes it out. An
  alarm existing implies nothing beyond what its level declares.
- A component **fills** each [system slot](/architecture/core-entities/#system-slots-what-a-system-needs-filled)
  it is assigned to unless its own health is `outage`. A degraded component still counts: severity is
  how loudly to tell somebody, not a second staffing threshold. The typed-slot guard (the component
  types a slot accepts, optionally pinned products) is checked once, at assignment, and plays no part in
  health.
- A slot filled below its **quorum** is **impaired** and contributes its **impact**. The system takes
  the worst contribution; a location takes the worst across its subtree.

A component's own health is the only thing that travels from the device that broke to the slot that
cares ([ADR-0050](/architecture/decisions/#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain)).
A component that fills no slot still records its own health, and moves nothing above it.

### A component nothing has observed

"No active alarms" is two different facts. A component something has **observed** and found clean is
`healthy`. A component that **nothing has ever observed** reads **`unknown`** on its own reads, because
`healthy` there would be a false green: a device created a moment ago, behind an endpoint no outpost has
probed yet, has had no chance to be wrong (#1454).

A component counts as observed once an alarm was ever raised on it (a cleared one counts) or one of its
endpoints has delivered a batch of anything (its `last_seen` stamp). Until then the component's health
read, its alarm list, the Explore outline and a system's member list all say `unknown`, so no two
surfaces disagree about the same device. A recorded health always wins, since recording one means the
component was observed.

This `unknown` is a reading of those reads alone. It never enters the rollup, so an unobserved member
neither raises nor hides its system's health.

:::design[Unknown in the rollup, tracked in #2608]
A stale or unmeasured member should show in its system's health. The design adds `unknown` off the
order, with a reason: **`stale`** (it had a value and lost it, the staleness watchdog on
[time](/architecture/time/)), **`no-data`** (covered, never reported) and **`uncovered`** (no rule that
moves health watches it). Every collected component seeds a baseline reachability rule, keeping
`uncovered` rare. How `unknown` composes upward is still open: a required slot whose only member is
unmeasured is not `healthy`, and calling its system an outage overstates it.
:::

## Impact lives on the slot

`impact` is a column on `system_slot`: `outage`, `degraded` or `none`, defaulting to `degraded`. It
belongs to the slot, not to the alarm or the component, because the same broken device matters
differently in different slots: a dead confidence monitor is not a dead main display.

| impact | an impaired slot means | use it for |
|---|---|---|
| `outage` | the system is not working | a slot the room cannot run without |
| `degraded` | the system works, worse | a slot that costs quality, not the meeting |
| `none` | nothing | a slot you track but do not depend on |

**Quorum is the redundancy knob.** A quorum of 1 with two components assigned tolerates one failure; a
quorum of 2 with two assigned is impaired the moment either goes out. Redundancy is the gap between
assigned and needed, with no separate vocabulary.

## The four values

```text
healthy  <  incomplete  <  degraded  <  outage
```

The order gives "worst" a meaning. `outage`, not `down`: a device is down, a room has an outage
([ADR-0003](/architecture/decisions/#adr-0003-health-reads-ok-not-up)).

**`incomplete` is a commissioning gap, not a fault.** A slot can be short of its quorum two ways. Its
assigned hardware can be failing, which is what `impact` describes and what an alarm fires for. Or the
hardware was never assigned, which no alarm will ever report, because nothing exists yet to alarm on. A
slot short for that second reason reads `incomplete`. Much of a real fleet is mid-commissioning for
months, and folding an empty slot into `outage` would paint it red and teach operators to ignore the
color. Ranked between `healthy` and `degraded`, a gap shows above a finished room and hides beneath
anything actually broken.

- A slot both under-assigned and partly failing reads its `impact`, the worse of the two.
- A slot whose impact is `none` reads `healthy` when empty: an absence that does not matter is not
  outstanding work.
- A slot inside a **choice** whose alternate did not win contributes nothing, `incomplete` included.

Health is not severity. Severity is how urgent an alarm is; health is what an entity is. A `high` alarm on
a component that fills no slot moves nothing above it.

## The rollup is a pure function

The judgement lives in `internal/health`, a package with no database access: storage resolves the
inputs (slots, assignments, component health, choices) and records the answer, and the package decides.
Every subtle case is a unit test there.

Two defaults are deliberate safety calls pointing in opposite directions:

- An **unrecognized impact reads `degraded`**, never `healthy`: a bad value must not make an impaired
  slot silently harmless.
- An **unrecognized recorded value reads `healthy`**: one stray row must not paint a fleet broken.

The rule behind both: fail loud about a judgement, fail quiet about a record. A system with no slots is
`healthy` (nothing is claimed about it), and a quorum below one reads as one.

## Health is recorded as a transition

Health history has to be accurate to the edge: exactly when a system went from healthy to broken,
readable weeks later. So health is computed at the **write** that changed it and recorded only when it
changes.

The carrier already exists. Each component, system and location owns a `health` series on the
[property lane](/architecture/properties/), which is transition-only by construction, written with
`provenance='calculated'` and `source_rule='health-rollup'` and read back along the
[owner arc](/architecture/core-entities/#ownership-the-exclusive-arc). There is no `health_history`
table: it would be a second, worse copy. An owner's first value is always recorded, even `healthy`, so
"healthy since we started watching" differs from "never evaluated".

Computing health on read would keep no history at all. Computing on read and writing the result through
is worse: the history looks real but is sampled by whoever opens a page, so a room that broke on Friday
night and was opened on Monday reads as breaking on Monday.

### Recomputed at the write, in its transaction

Every mutation that can move health recomputes it inside the caller's transaction, so the cause and the
health commit together or not at all:

| the write | why it can move health |
|---|---|
| raise or clear an alarm; change a severity level's health effect | a component's own health, and every slot it fills, can flip |
| assign or unassign a component | a slot reaches or falls below its quorum |
| declare or withdraw a slot | a system gains or loses a slot it can be short of |
| change a slot's quorum or impact | the same staffing crosses a different line |
| create a system | its opening health starts its history |
| change the standard a system conforms to | its inherited slot set is swapped, its staffing carried onto the slots that remain ([ADR-0506](/architecture/decisions/#adr-0506-changing-a-systems-standard-carries-its-staffing-onto-the-roles-that-still-exist)) |
| move a system to another location | the contribution moves, so both locations recompute |
| move a location under another parent | its subtree's contribution moves, so both ancestor chains recompute ([ADR-0092](/architecture/decisions/#adr-0092-a-location-move-recomputes-both-ancestor-chains)) |
| delete a system | the location it sat in may have just improved |

The location a system or a location **left** is named explicitly, because a recovery is an edge as real
as a failure. A location holding anything cannot be deleted, so a deletable location is empty and needs
no recompute. A component's product edit does not move health: the typed-slot guard is checked only at
assignment.

A **composite write** (a planned system with its slots and components, in one transaction) recomputes
once, before commit, over the union of what it touched. Recomputing per piece would record `incomplete`
and then `healthy` milliseconds apart for a system born empty and filled slot by slot: an edge nobody
could observe, since the write is atomic.

A missing trigger is a hole in the history. That is the honest cost of this design, and why the list is
enumerated in code rather than inferred.

### Owners lock by id, in one order

A recompute takes a transaction-scoped advisory lock on every owner it visits and holds it to commit, so
a concurrent recompute of the same owner reads this one's committed result. Two recomputes with
overlapping chains therefore contend, and what keeps contention from becoming deadlock is that both
visit owners in one order: **components, then systems, then locations, each ascending by id**. The key
is the id because names are unique only within their parent: two rooms under different buildings may
both be `415a`, and an order that can tie is no order at all.

### A location rolls up from its children

A location's recorded health is the worst of its **direct children's recorded health**: the systems in
it and the locations under it, one latest row each
([ADR-1562](/architecture/decisions/#adr-1562-a-location-rolls-up-from-its-childrens-recorded-verdicts)).
That equals the worst over its whole subtree, because the rollup is worst-wins and each child location
already holds the worst of its own. A write pays for the chain it touches, not for the fleet under each
location. It holds only while every child is current, so a recompute locks its whole chain first, then
rolls it up bottom-up. The health reads still fold the subtree live, and the tests hold the recorded tree
equal to that fold after every kind of write.

### A read never writes

Healing on read would stamp the edge at read time, the very inaccuracy this model avoids. A read does
compute the health it serves from the same rows it displays, so a report can never say `healthy` beside
an impaired `outage` slot. Recorded transitions stay the source of history: a missing trigger can cost
an edge, never a report that misstates the present.

## Reading health

Health is read in two shapes, and which one a surface takes is a rule: **a list never pays for an
explanation it does not render**.

- **The explanation**, one owner at a time: `GET /systems/{name}/health`, `GET /locations/{name}/health`
  and `GET /components/{name}/health` (`omniglass component health list <name>` from the CLI). It is
  what a panel opens into, and it is expensive by design, because explaining is the job.
- **The bulk health**: `GET /systems:health` returns one `{system, health}` entry per system in the
  caller's read scope, in one statement whatever the fleet size, and is what a list paints its health
  column from (#653). A system with nothing recorded reads `healthy`, which is not a guess: health is
  written only on a transition, and a system that has never been recomputed has no impaired slot. The
  console's `HealthBadge` takes a health from its caller and fetches only when nobody has one.

Every read is scope-injected, and an owner outside the caller's scope is a non-disclosing 404
([API](/architecture/api/)).

**A system's explanation** carries its health, a `reason` line naming the slots behind it, every slot it
needs filled and, for an impaired slot, the causing chain: "the `room-mic` slot wants 2 and has 1,
because `mic-pod-2` went out on a `high` alarm raised at 14:02" tells the operator where to walk. A slot
impaired with no assigned component out names no alarm, which tells **short-staffed** from **broken**.
Each causing alarm carries `acknowledged`, `acknowledged_at` and `acknowledged_by`, so the system page's
Why strip shows who has seen it and offers **Acknowledge** on the rest (#990); acknowledgement annotates
an alarm and never moves health
([acknowledgement](/architecture/alarms-actions/#acknowledgement-a-fact-about-a-person)).

It also lists `members`: every component bound into the system, with its own health, its open alarms and
the slot it fills, or `outside_slot`. A member outside every slot is **shown, not rolled up**
([ADR-2459](/architecture/decisions/#adr-2459-an-alarm-on-a-member-outside-a-slot-is-shown-on-its-system-not-rolled-up)):
its alarms appear on the system so nothing is hidden, and they move nothing above the component, since
the slot and its impact are the system's health rule.

**A location's explanation** is its health plus every system beneath it with its own health: a map, not
a duplicated explanation. **A component's** is the same envelope with no slots and no systems.

All three carry the **recorded transitions**, oldest first, one entry per change: the availability
strip's data and the answer to "since when". The range is `since` and `until` (RFC 3339), defaulting to
the last 30 days; `since` after `until` is a 400. `carried_in` is the health in force as the range
opened, on an explicit `since` and on the default window alike (#2735); only a whole-history read, which
has no start, carries none. `first_recorded_at` says when the owner was first recorded.

The console renders health as **HealthBadge**, **HealthHistory** and **AlarmsPanel** on the component,
system and location detail views. Healthy reads green, and a muted green in the outline, so it never
reads as the grey of an unknown health
([ADR-0154](/architecture/decisions/#adr-0154-healthy-reads-green-tiered-so-a-calm-fleet-stays-calm)).

## Where health goes next

:::design[Alarms owned by a system or a location, tracked in #1002]
An alarm is component-only today. The design gives an alarm the same exclusive-arc owner a sample has,
so a system-scoped rule reading member data raises a **system-owned** alarm for a fault only the system
cares about: a display on input 2 is normal for the display and wrong for the room, and the component
stays generic. A vendor's "offline" from a shared API follows the same discipline: an observed signal to
write system rules over, never a health of its own. The chain stays acyclic: an alarm that feeds health
moves its owner's own health, an alarm raised off the `health` state moves nothing, and a calc rule
reading `health` is refused, so health only rolls up.
:::

:::design[Service levels, KPIs and the fleet top, tracked in #2607]
**The `global` owner.** The rollup ends at a top location today. A singleton `global` owner above every
location carries fleet-wide health and KPIs; the owner gives a reading its level, so one `health` key
serves every tier.

**SLI, SLO, SLA.** A service level indicator is a derived value over a window of the recorded health:
availability is the share of the last 30 days a system read `healthy`, exact and cheap over a series of
edges. The SLO is the target (availability at least 99.9%), a [variable](/architecture/variables/) on
the entity or its standard. The SLA is meeting it: an event rule fires when the SLI breaches the
target. Rolling windows serve trends; calendar windows (the billing month, in an IANA zone) serve
contracts, riding the clock on [time](/architecture/time/).

**KPIs.** A KPI is a derived value registered as canonical and owned at the level it describes. The
shipped set starts with availability at every level, and adds the AV-native utilization family once
occupancy and booking signals exist: occupancy against capacity, used against idle minutes, booked
against unbooked minutes, and ghost meetings (booked, nobody came).

This is the Zabbix service tree done as one model rather than a separate subsystem: the system tree is
the service tree, health is a recorded sample, the SLI a calc over it and the SLA an alarm. How a parent
system relates to its subsystems' health is decided with system grouping (#879).
:::

Related: [core entities](/architecture/core-entities/#system-slots-what-a-system-needs-filled) for
slots, quorum and the typed-slot guard; [alarms and actions](/architecture/alarms-actions/) for what
raises an alarm; [metrics and properties](/architecture/properties/) for the property lane;
[standards](/admin/standards/) for the slots a standard declares.
