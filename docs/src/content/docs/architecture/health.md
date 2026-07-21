---
title: Health, KPIs, and service levels
description: "Health as a verdict rolled up from alarms through capabilities and roles, recorded as a transition so the edges are accurate, plus the KPIs every estate should track and SLI / SLO / SLA."
sidebar:
  badge:
    text: Partial
    variant: note
---

Health gives an operator the one answer that matters most, "is this system working right now?", and the
one that matters next, "since when?". Omniglass is **opinionated about health**: it is a **first-class
capability**, not a byproduct of a customizable rules engine. The *model* is deliberate (an alarm degrades
a capability, a capability failure impairs a role, an impaired role sinks its system by a declared
impact); the *carrier* is the ordinary datapoint pipeline, so health is stored, queried, and trended like
any other signal, with no parallel subsystem.

:::note[Partial]
Built today: the **`alarm`** table (component-local, with the capabilities it degrades), **`impact`** on a
`system_role`, the **rollup** from component through system to location, and the **recorded transition
history** that answers "since when". Two reads serve it (`GET /systems/{name}/health` and
`GET /locations/{name}/health`) alongside the alarm write surface on a component. Still `Design`: alarms
raised by an [`event_rule`](/architecture/alarms-actions/) rather than by a caller, system- and
location-owned alarms, the `unknown` verdict and its coverage reasons, the **`global`** estate top, and
the whole **SLI / SLO / SLA** and **KPI** tier below
([ADR-0050](/architecture/decisions/#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain)).
See [implementation status](/architecture/status/).
:::

## The chain: capability is the routing key

Health is not "the worst thing happening near this room". It is a chain with one hop per question, and
every hop is a thing an operator already models:

```text
alarm on a component
  -> degrades named capabilities
    -> a component no longer satisfies a role that required one
      -> the role falls below its quorum and is impaired
        -> the role contributes its declared impact
          -> the system takes the worst contribution
            -> the location takes the worst of its systems
```

- An **[alarm](#the-alarm-what-is-wrong-with-one-component)** is **component-local**: something is wrong
  with this box.
- It names the **[capabilities](/architecture/core-entities/#system-roles-the-slots-a-system-needs-filled)**
  it degrades. That naming is the only route out of the component. An alarm that degrades nothing is a note
  on the device and reaches no system, which is the honest reading, not a special case.
- A component **satisfies** a [system role](/architecture/core-entities/#system-roles-the-slots-a-system-needs-filled)
  only when it **provides every** capability the role requires **and none of those is currently degraded**.
  A capability it has on paper but cannot deliver right now does not count.
- A role with fewer satisfying components than its **quorum** is **impaired**.
- An impaired role contributes its **impact**, and the system takes the **worst** contribution among its
  roles. A location takes the worst among every system placed anywhere in its subtree.

This is why a **capability is flat** and why a **role requires a set** of them. Capability is the only
vocabulary shared by the thing that breaks (a component) and the thing that cares (a slot in a room), so
it is the only honest place to route a failure through. Everything else about a component (its vendor, its
model, its properties) describes it; only its capabilities say what a room loses when it stops working.

## Impact lives on the role

**`impact`** is a column on `system_role`: `outage`, `degraded`, or `none`, defaulting to `degraded`. It
answers "what does this slot being empty mean for this room?".

It lives on the **role**, not on the alarm and not on the component, because the same broken box matters
differently depending on the slot it was filling. **A dead confidence monitor is not a dead main display**,
and the difference is not a property of the display, it is a property of the job it was doing. Declaring
impact on the role puts the judgement exactly where the operator already made it.

| impact | an impaired role means | use it for |
|---|---|---|
| `outage` | the system is not working | the slot the room cannot run without |
| `degraded` | the system is working, worse | the slot that costs quality, not the meeting |
| `none` | nothing | a slot you track but do not depend on |

**Quorum is the redundancy knob.** A role with quorum 1 and two components assigned tolerates one failure
with no verdict change, because one satisfying component still meets the quorum. A role with quorum 2 and
two assigned is impaired the moment either one degrades. Redundancy is therefore not a separate concept
with its own vocabulary, it is the gap between how many you staffed and how many you need.

## The verdict vocabulary

A verdict is one of three values, ordered so "worst" has a meaning:

```text
healthy  <  degraded  <  outage
```

**`outage`, not `down`.** A device is down; a room has an outage. The word is chosen for what a broken
system means to the people standing in it, which is the same reasoning that once picked `ok` over `up`
([ADR-0003](/architecture/decisions/#adr-0003-health-reads-ok-not-up)), applied one level further out.

Health is **distinct from severity**. Severity is an alarm's alert importance
([alarms and actions](/architecture/alarms-actions/)); health is an entity's operational state. They
correlate but they are different axes: a `critical` alarm on a component filling no role moves nothing
above that component, and an `info` alarm that degrades the one capability a required role needed takes
the room out.

## The rollup is a pure function

The judgement lives in a **pure package** (`internal/health`) that takes resolved inputs and returns a
verdict, with **no database access at all**. Storage resolves the inputs and records the answer; the
package decides. The subtle cases (a quorum boundary, a role nobody staffed, an alarm degrading a
capability no role wanted) are exactly the ones that go quietly wrong in SQL and are trivial to pin down
in a unit test, which is the whole argument for the split.

Two of its defaults are deliberate safety calls, and they point in **opposite** directions:

- An **unrecognized impact reads `degraded`**, never `healthy`. A bad value must not make an impaired role
  silently harmless.
- An **unrecognized recorded value reads `healthy`**. One stray row must not paint an estate broken.

The rule behind both: **fail loud about a judgement, fail quiet about a record**. A judgement that cannot
be trusted should raise its hand; a record that cannot be parsed should get out of the way.

Two more defaults follow the same instinct. A **system with no roles is `healthy`**, because nothing has
been claimed about it and painting every unmodelled system red would train operators to ignore the color.
A **quorum below one is treated as one**, because a role no component need fill is not a role.

## Health is recorded as a transition

This is the load-bearing part of the design.

> The most important thing about health is that we have a real, accurate history of the edges. We need to
> know exactly when a system went from healthy to unhealthy, and be able to look back at it weeks later.

Everything else follows from taking that literally. If the history has to be **accurate**, the only correct
place to compute a verdict is the **write** that changed it. If the history has to be **edges**, the right
carrier already exists.

Health lands in **`state_datapoint`**, which is **already transition-only**: the ingest path writes a row
only when the value differs from the last one stored for that owner, and `StateTransitions` reads the
ordered flips that draw the reachability availability strip. Health reuses that primitive exactly as it
is, on the **[owner arc](/architecture/core-entities/#ownership-the-exclusive-arc)** (a component, a
system, or a location owns its own health series), with `provenance='calculated'` and
`source_rule='health-rollup'` naming the producer in the row's lineage. There is **no `health_history`
table**, because it would have been a second and worse copy of one that already exists.

The first value for an owner is **always** recorded, even `healthy`. An owner whose history starts at its
first health-relevant write has a defined beginning; recording only once something goes wrong leaves a
reader unable to tell "healthy since we started watching" from "never evaluated".

### Two alternatives, and why both fail

**Compute the verdict on read.** Cheap, always current, no writes. It keeps **no history at all**, so
"when did this break?" is unanswerable by construction. That is not a smaller version of the requirement,
it is the opposite of it.

**Compute on read and write the result through.** This looks like it solves the first one, and it is the
more dangerous idea because it produces a history that **looks** real. The history is **sampled by whoever
opens a page**: the recorded edge is stamped at the moment somebody **looked**, not the moment the estate
**changed**. A room that broke on Friday night and was opened on Monday morning reads as breaking Monday
morning, and a weekend nobody watched has no weekend at all. A history whose timestamps mean "when a human
navigated here" is worse than no history, because it will be trusted.

### Recompute at the write, in the same transaction

A verdict is recomputed by **every mutation that can change it**, inside the caller's transaction, so the
cause and the verdict commit together or not at all:

| the write | why it can move health |
|---|---|
| raise or clear an **alarm** | a capability is taken away or given back |
| **assign** or **unassign** a component | a role reaches or falls below its quorum |
| **declare** or **withdraw** a role | a system gains or loses a slot it can be short of |
| change a role's **quorum** or **impact** | the same staffing crosses a different line |
| add, suppress, or clear a **component capability** | what the component provides is half of satisfying a role |
| change a component's **product** | the product supplies its default capabilities |
| **create** a system | its opening verdict gives its history a beginning |
| change the **standard** a system conforms to | the whole inherited role set is swapped |
| change a system's **location** | the contribution moves between rollups, so **both** are recomputed |

A declaration change on a **standard** moves **every conforming system** at once, since they inherit it
live. The relocation case names the location the system **left** explicitly, because that location's row no
longer points at the system and its rollup may have just **improved**: a recovery is an edge as real as a
failure.

A **missing trigger is a hole in the history**, which is the honest cost of this design and the reason the
trigger list is enumerated rather than inferred.

### A read never writes

Self-healing on read would stamp the edge at read time, which is precisely the inaccuracy this whole model
is built to avoid. So the reads write nothing.

They do, however, **compute the verdict they serve from the same rows they display**. This was a
correctness fix: the report originally served the **last recorded** verdict while resolving the
contributing roles **live**, which let a system with nothing recorded yet report `healthy` while listing an
impaired `outage` role right beside it. The report contradicted its own evidence, which is worse than no
report. Deriving the served verdict from the resolved rows costs nothing (they are already loaded) and
makes that class of contradiction impossible. **Recorded transitions remain the source for history**, which
is a different question. A missing trigger can therefore cost an **edge**, but it can never make a report
**lie about the present**.

## Reading health

Two reads, both scope-injected, both a non-disclosing 404 for an owner outside the caller's scope
([API](/architecture/api/#health-the-verdict-and-why)).

**A system's report** is the verdict, every role it needs filled, and for an impaired role the causing
chain: which required capabilities an active alarm has taken away and which alarms took them. That chain is
the point. A bare "degraded" gives an operator nothing to do; "the `room-mic` role wants 2 and has 1,
because `mic-pod-2` lost `microphone` to a critical alarm raised at 14:02" tells them where to walk.

A role can also be impaired with **no alarm to name**: nobody was assigned, or the assignments never
provided what it requires. The report says so by naming no degraded capability, which distinguishes
**short-staffed** from **broken**, two very different jobs.

**A location's report** is the verdict plus every system beneath it with its own verdict, as the
drill-down. The system read explains the rest, so the location report stays a map rather than duplicating
the explanation at every level.

Both reports carry the **recorded transitions** over the last 30 days, oldest first: one entry per change,
never a sample. That is the availability strip's data, and the answer to "since when".

## The alarm: what is wrong with one component

An alarm is a row on a component with a **`severity`** (`info`, `warning`, or `critical`), a **message**,
a **`raised_at`**, and a **nullable `cleared_at`**. Clearing sets `cleared_at` and **keeps the row**: what
was wrong, and when, outlives the fix. Clearing an alarm that is already cleared is an explicit miss, not a
silent success.

Severity drives the **component's own** verdict (any active alarm makes the component `degraded`, a
`critical` one makes it an `outage`) and **nothing above it**. What reaches a system is the **capability
set**, not the severity. This separation is deliberate: severity is how loudly to page somebody, impact is
what the room lost, and conflating them is how a monitoring system ends up with a critical alert about a
spare device nobody uses.

A component's verdict is recorded on its own arc like any other, so a component that fills no role still
carries an accurate history of what was wrong with it.

## Still design: where alarms come from

Today an alarm is written by an **operator or an API caller**. The full model has them produced by the
detection tier: an [`event_rule`](/architecture/alarms-actions/) watches datapoints, fires an event, and an
alarm **opens** and stays open while its condition holds, closing on the paired clear event. Health is
**ack-independent**, because ack is not close: an acknowledged alarm stays open while its condition holds,
so acking annotates and never makes a broken room look healthy.

That tier is what turns health from a modelled verdict into a **measured** one. The chain above does not
change when it lands: a rule-opened alarm names the capabilities it degrades exactly as a hand-raised one
does.

:::caution[Open question]
Whether a rule declares the degraded capability set directly, or derives it from the datapoint it watched.
:::

### Alarms owned by a system or a location

The alarm arc is **component-only** today. The design gives an alarm the same
[exclusive-arc owner](/architecture/core-entities/#ownership-the-exclusive-arc) every datapoint and event
has, so a **system-scoped** rule can raise a **system-owned** alarm over member data. The canonical case: a
display sitting on **input 2** is a perfectly normal state *for the display*, but in a specific room it
means the wrong source is on screen. The system template owns the conditions only the system cares about,
and the component stays generic.

The same discipline governs **SaaS and vendor status** (a UCC platform mapped to system-owned datapoints,
[shared-API collection](/architecture/collection/)): a vendor's reported "offline" is an *observed signal
from one source*, not a verdict on the room. Author the system condition over it, **corroborated** where
you can, rather than trusting it. The vendor's opinion is an input to health, not health itself.

The acyclic discipline holds either way: an alarm that **feeds** health degrades a capability, and the
"system is down" alarm that fires **off** health (a rule watching the `health` state) degrades nothing.
Inputs route, consequences do not, and health rolls up only, so there is no loop.

### `unknown`, and honest coverage

The built domain has three values and no `unknown`. The design adds a fourth reading, **off the order**,
for "nothing here knows what failure looks like":

- **covered, nothing firing, data fresh -> `healthy`.** Something measures what broken means here, it is
  watching, and it is silent.
- **not covered -> `unknown`.** No health-impacting rule resolves. Reporting `healthy` would be a false
  green.

`unknown` carries a **reason** discriminator as metadata, so an operator can tell a measurement gap from a
coverage gap: **`stale`** (had data, it went stale, the no-data machinery in [time](/architecture/time/)),
**`uncovered`** (no health-impacting rule resolves), **`no-data`** (a rule covers it, but it has never
reported). To keep `uncovered` rare rather than default, every **collected component** is seeded with a
baseline reachability alarm, so a freshly-collected device is covered on its first poll.

:::caution[Open question]
How `unknown` composes upward. A required role whose only component is unmeasured is not `healthy`, but
calling the system an outage overstates it.
:::

### The `global` estate top

The rollup ends at a location today. The design adds the singleton **`global`** owner above every location,
the estate-wide verdict leadership reads and the owner that estate-wide KPIs hang off. The same `health`
key flows the whole way up: the **owner** gives a reading its level, so one key serves component, system,
location, and global without cross-triggering.

## SLI: indicator over a window

A **Service Level Indicator** is a `time_in_state` calc over a window (`time_in_state(s)` = the fraction of
the window the entity held state `s`, derived from the health transitions this page records), emitted as
its own datapoint (the temporal reducer, [expressions](/architecture/expressions/)):

```yaml
# availability = fraction of the last 30 days the system was healthy
source: { datapoint: health, over: 30d }
reduce: time_in_state
when: "value.healthy / value.total"   # an Expr leaf shapes it into a ratio
# -> emits system.availability
```

An SLI is therefore just another derived datapoint, queryable and trendable like any other. It is also the
clearest payoff of transition-only recording: `time_in_state` over a stream of edges is exact and cheap,
where the same calc over samples is an approximation whose accuracy depends on who was looking.

## SLO and SLA: the target, and meeting it

Three terms, not two. The **SLI** is the *measured indicator* (the `system.availability` calc above). The
**SLO** (Service Level Objective) is the **target**: the number you intend to hold (availability >= 99.9%),
a [config](/architecture/variables/) value on the entity or standard, not machinery. The **SLA** (Service
Level Agreement) is **meeting the SLO**: an `event_rule` fires when the SLI breaches the target, and
compliance over the contractual window is itself an SLI.

```yaml
event_rule:
  scope: 'system.standard == "meeting-room"'
  datapoint: system.availability
  when: "value < $var:availability.slo"   # the SLO target, a config value
  severity: high
```

So the target is config (the SLO), the breach is an event and alarm (the SLA edge), and compliance is a
calc (an SLI over the SLA). No new machinery. Windowing is the SLI's concern: a **rolling** window (last
30d) for trends, or a **calendar** window (the billing month) for a contractual SLA; the calendar reset is
the one piece that leans on the time primitive.

:::caution[Open question]
The SLA calendar-window boundaries and timezone, co-designed with the time primitive.
:::

## KPIs: what every estate should track

A **KPI** is a derived datapoint (a calc or SLI), registered as a canonical property and owned at the level
it describes (system, location, or **global**). It is no new primitive: a KPI is a shipped calc the same
way health is. Omniglass ships an opinionated **default set** so the data is there out of the box, with the
escape hatch to author your own.

**Availability** is health over time: the SLI `time_in_state(healthy)` above. Health is the substance,
availability is its ratio, so it ships free at every level up to global.

**Utilization** is the AV-native family, over occupancy and booking data:

- **occupancy**: current people / capacity (an instant ratio);
- **time-utilization**: used vs idle minutes;
- **booking-utilization**: booked vs unbooked minutes;
- **ghost**: occupied vs booked, so booked but nobody showed (the wasted-room signal).

Both inputs are **ordinary components**, no special integration: an occupancy sensor emitting `occupancy.*`
and the booking system, a component whose interface is the calendar API, emitting `booking.*`. The KPIs are
then calcs over those datapoints, owned at room / system / location / global like any rollup. A booking API
is just an interface; a ghost meeting is just `occupied < booked`.

:::caution[Open question]
The full default KPI set and each one's exact calc. Availability and the utilization family are named, but
the precise reducers and windows are unsettled.
:::

:::caution[Open question]
The `occupancy.*` and `booking.*` canonical signals, and the occupancy-sensor and booking-system component
templates that feed the utilization KPIs.
:::

## Why this is the Zabbix service tree, done right

Zabbix bolts services, SLA, and the service tree on as a separate subsystem. Omniglass does the opposite:
health is **first-class but not separate**. The model is opinionated (an alarm degrades a capability, a
role declares its impact, the rollup is engine behavior rather than an editable reducer) and it rides the
one datapoint pipeline, so the **system tree is the service tree**: the verdict is a state datapoint, the
history is its transitions, the SLI is a calc over them, and the SLA is an alarm. One model, composed,
instead of a parallel feature. An operator who understands alarms and datapoints already understands health.

Related: [core entities](/architecture/core-entities/#system-roles-the-slots-a-system-needs-filled) (the
role, the capability, and the quorum), [alarms and actions](/architecture/alarms-actions/) (the detection
tier that will raise alarms), [datapoints](/architecture/datapoints/) (the state datapoint and the owner
arc), and the [Standards](/guides/admin/standards/) and
[Work with an entity](/guides/operator/entities/) guides for the operator loop.
