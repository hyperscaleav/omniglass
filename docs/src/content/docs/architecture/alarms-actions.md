---
title: Alarms and actions
description: How Omniglass detects a condition with a stateful alarm and responds with an action, which is a flow when it has more than one step.
sidebar:
  badge:
    text: Design
    variant: caution
---

Alarms and actions are how Omniglass turns a detected condition into a held incident and then into a response, so an operator gets paged, a ticket opens, or a device gets fixed without anyone watching a dashboard. An **alarm** detects a condition and holds it; an **action** does
something about it. A simple action is a single step (a `notify`); a multi-step action is a
**flow** (it branches, waits, and runs steps in parallel). Both alarm and action are **stateful
entities that hold their state directly** (not event-sourced). The credentials an action uses to reach a sink live in
credentials; the Expr and Go-template machinery in
[expressions](/architecture/expressions/).

## The alarm (a stateful entity)

**Metaphor: a morning alarm, not a pop-up.** A pop-up *alert* appears and is gone
(that is a `notify` action). An **alarm** goes off when its condition is met and
**stays high until it is interacted with**. The `alarm` row **holds its current
state directly** (`status`, `severity`, `opened_at`, `resolved_at`, `acked_by`); it
is **not** event-sourced. It is **one incident, a new row per open**, keyed by
`(event_rule, owner)` (the exclusive-arc owner, so a system- or location-owned datapoint
yields a system/location-owned alarm), the ITSM correlation anchor ([datapoints](/architecture/datapoints/)).
The open and resolve **events** carry the `alarm_id` and are the edge log; the alarm
row is the live state.

The alarm is **PG-first**: the firing `event_rule` consumer writes the stateful alarm
row in the **same Postgres transaction** as the event, with the alarm transition
**serialized per `(event_rule, owner)`** so an incident never double-opens. A
leader-elected CDC publisher (logical decoding of the WAL) then publishes the committed
open / resolve transition to JetStream, where `action_rule` consumers react. Born in the
commit, fanned out by CDC: there is no dual-write, and Postgres is never a message bus.

Transitions, by who drives them:

- **opened / resolved** are **rule-driven** (the `event_rule`'s `fire_criteria` /
  `clear_criteria`), each emitting an `event` carrying the `alarm_id`;
- **acked / snoozed** are **operator-driven**, recorded in `audit_log` (also carrying
  the `alarm_id`) and applied to the alarm row.

The full timeline assembles by `alarm_id` across events + audit; the alarm row is
never reconstructed from them.

Alarms are **terminal upstream**: they never write datapoints, so they cannot feed
back into the datapoint layer (see *Cycle safety*).

## The `event_rule`

An `event_rule` carries a required `fire_criteria` and an optional `clear_criteria`.
With a `clear_criteria` the fire event **opens** an alarm and the clear
event **resolves** it; without one the rule is momentary (a one-shot event, no
alarm). There is no separate `alarm_rule`.

```yaml
event_rule:
  scope: 'component.template == "polaris-dsp-16"'   # the shared selector (expressions)
  datapoint: dsp.temperature
  window: { reduce: avg, over: 10m }                 # machinery (optional)
  fire_criteria: "value > 65"                        # opens the alarm
  clear_criteria: "value < 60"                       # resolves it (defaults to !fire_criteria)
  for: 0                                             # fire-side sustain (optional)
  for_clear: 0                                       # clear-side sustain (optional)
  severity: average
  health: degraded                                   # optional: degrade the owner's health while open
```

`scope` selects the entities (fan-out, one alarm per match); `datapoint` is the
input; `window` / `for` / `for_clear` are the aggregation machinery; `fire_criteria` / `clear_criteria`
are the Expr leaves; `severity` names a level by id (below). A rule is **suppressible by name through
the cascade** ([cascade](/architecture/cascade/)): a high-weight group can remove a
false-firing rule without editing it (the firmware-bug workaround).

`for` and `for_clear` are **symmetric sustains** on the two edges. `for` is the
fire-side sustain: the `fire_criteria` must hold for `for` before the alarm opens.
`for_clear` mirrors it on the recovery edge: the `clear_criteria` must hold for
`for_clear` before the alarm resolves, so a source flapping at the cadence boundary
does not churn the alarm open and clear. Both default to `0` (immediate), and a rule
can set them independently (a long `for_clear` over a short `for` holds an incident
through a noisy recovery without delaying the page).

An event_rule also carries an optional **`health` impact** (`down` / `degraded`,
default none): while the alarm it opens is open, it moves its owner's
[health](/architecture/health/) by that much. Most rules carry none (an advisory
alarm); the few that do are the owner's failure conditions. This is what makes health
**alarm-sourced** rather than a parallel computation. Because a rule is scoped to
whichever arc owns its datapoint, the same machinery yields **component-level** and
**system-level** alarms: a system-scoped rule reads member data and fires a
system-owned alarm for a condition only the system cares about (a display on input 2
is fine for the display but wrong for the room), which is how system health sees what
no single component can.

## Severity: a registry of named levels

Severity is a **registry of named levels**, not a bare integer. Each level has an
**`id`** (`info`, `warning`, `average`, `high`, `disaster`), a **`label`**, a **`color`**,
and an integer **`order`** used only for comparison. Operators and rules reference a level
**by id**; the order ranks them. Official defaults ship, **spaced** so a new level inserts
by picking an order between two others, no renumbering:

| id | label | color | order |
|---|---|---|---|
| info | info | gray | 10 |
| warning | warning | yellow | 20 |
| average | average | orange | 30 |
| high | high | red | 40 |
| disaster | disaster | dark red | 50 |

Severity is **distinct from health**: severity is alert importance, health is entity
operational state ([health](/architecture/health/)), different axes. Higher order is
more severe. The level set is **operator-customizable**: an operator can relabel to
P1/P2/P3, recolor, add a level between two others, or define three levels or seven, all
config, no code. Rules and `action_rule` predicates compare **by level**, resolved through
the order (`alarm.severity >= "high"` matches `high` and `disaster`); the UI renders the
label and color from the level.

:::caution[Open question]
Whether a severity level is purely a label, color, and order, or also carries policy such as a
default ack-timeout per level.
:::

## The action (a stateful entity)

What an `action_rule` raises and runs. Like an alarm it is **stateful** and holds
its own state directly (status, current step, delivery), not event-sourced:

- **kinds**: `notify` (in-app), `webhook`, `email`, `run` (execute a command; the
  edge realization is in [templates](/architecture/templates/) / [nodes](/architecture/nodes/)).
- a **simple** action carries delivery state (`queued / sent / failed / retried`,
  the at-least-once JetStream consumer);
- a **multistep** action is a **flow**: it carries workflow state (current step, waiting,
  branches), exactly like an alarm's lifecycle.

The `action` row carries identity, kind, config, and current status.

## The `action_rule` (decoupled subscription)

Detection and response are kept **separate**, the discipline that avoids Zabbix's
action/operation tangle: the `event_rule` does not contain its response. Instead an
`action_rule` is a **NATS consumer** on the CDC-published event / alarm stream, selecting
with an Expr predicate, so one action rule can serve many alarms. Subscriptions are **indexed by event key and label**, so dispatch
evaluates only the predicates whose key or label already matches, not every rule on every event;
an action rule may carry **multiple triggers** and fires if any matches (including a label or
wildcard trigger, e.g. any event labeled `room=boardroom-a`). It is a subscription, not a fourth datapoint-pipeline
rule family (the derivation rules, calc and event, produce data; the
`action_rule` wires the resulting events and alarms to actions):

```yaml
action_rule:
  on: alarm
  when: 'transition == "open" && alarm.severity >= "high" && component.type == "device"'
  action: pagerduty-notify
```

The **source is polymorphic** but guarded (see *Cycle safety*): an alarm transition
(open / resolve, each an `event` carrying the `alarm_id`), a scheduled fire (an
`event` with `origin=scheduled`, not a separate source), an operator (manual), and
the declarative runbook step-list. Bodies are **Go templates** and sink auth is a
**credential reference**, both per [expressions](/architecture/expressions/) and
credentials.

## Storm and dependency suppression

The alarm grain stays **`(event_rule, owner)`**: one upstream fault still fans out to
one alarm per affected owner. Two primitives keep that fan-out from becoming a page
storm without collapsing the grain.

**Dependency suppression** mutes a child alarm whose owner's **parent entity on the
[exclusive-arc](/architecture/datapoints/) structural tree** is itself down. When the
parent is in a `down` health state, the child alarms beneath it are held suppressed
(open, but not dispatched), so one upstream failure does not emit N child pages. It is
expressed over the exclusive-arc tree: the same arc that owns a datapoint and its
alarm gives the parent walk for free, no separate dependency graph.

**Action-level grouping** coalesces alarms sharing **owner / label / `correlation_id`**
into one **action dispatch**: one ticket with N members, not N tickets. The alarms stay
distinct rows at the `(event_rule, owner)` grain; grouping happens at the dispatch edge
in the `action_rule`, so a storm becomes one notification carrying the member list.

A **system-scoped `event_rule`** is the sanctioned upstream-cause dedup lever. Because a
system-scoped rule reads member data and fires a **system-owned** alarm for the
room-level cause (above), it names the actual fault once at the level that owns it.
Worked example: a switch reboot downs 20 endpoints. A system-scoped rule owns the
room-level cause as a single system-owned alarm; dependency suppression mutes the 20
child endpoint alarms whose parent (the room or switch) is down; action-level grouping
coalesces whatever child alarms remain into one dispatch. The operator sees the cause,
not 20 symptoms.

## Durability and egress

Action state is **PG-first + CDC-out**: the action's step transition is written to the
`action` row in a Postgres transaction, and the leader-elected CDC publisher fans the
committed change onto JetStream. The **external send** is then a **JetStream consumer**
(retry with backoff, dead-letter), not a Postgres `SELECT ... FOR UPDATE SKIP LOCKED`
relay. External sends are **at-least-once**; sinks tolerate dupes or we add an
**idempotency key** (alarm + action + transition) so the outcome is exactly-once.
Pipeline order: **render the body, then apply auth over the rendered bytes** (HMAC signs
the rendered body), then send. **Egress safety** is always on: block internal / metadata
IPs, verify TLS, bound timeouts, control redirects.

:::caution[Open question]
The dead-letter surface and the operator retry of failed actions.
:::

:::caution[Open question]
The observed-use auth-failure feedback from actions into credential health (paired with the
credential-health model in [config and credentials](/architecture/variables/)).
:::

## Cycle safety in the action layer

The `collection -> datapoint -> alarm` core is acyclic by construction (see *Cycle
safety*), and **only data authors events**: an `event_rule` over datapoints (plus the
clock's `origin=scheduled`) is the *only* way an event enters the log. Flows and
actions never manufacture events, so the response layer cannot inject into the event
graph at all. That leaves a single possible loop, the **data-mediated control loop**
(an action commands a device, the device's new state arrives as a datapoint, which
opens an alarm, which fires the action again), closed with three rules:

1. **Alarms are terminal upstream** (they never write datapoints), so detection cannot
   feed itself directly.
2. **ack / snooze transitions do not match `action_rule`s** (only open / resolve do),
   which breaks the `action -> alarm(ack) -> action` loop.
3. **The control loop is lineage-guarded at dispatch.** Before an `action_rule` runs
   an action, the engine walks the triggering event's **causation lineage** (carried on
   **NATS message headers** across the bus); if the same `(action, owner)` already
   appears upstream it is suppressed, with a depth bound as a backstop. Flows are finite
   by construction (a step list, per-open-alarm, gated on open, cancelled on resolve /
   ack).

The walk crosses the command-to-device round trip because the command **stamps its
originating `correlation_id` onto the intended write and onto the adaptive-poll's
observed datapoint** ([datapoints](/architecture/datapoints/)). The `event_rule` that
fires off that observed datapoint inherits the id, so the lineage walk follows a real
carried id across the device edge rather than an assumed lineage; the depth bound stays
as the backstop.

The carrier crosses the lane boundary, not one continuous header hop. The `event_rule`
writes the triggering datapoint's `correlation_id` **and a `caused_by_event_id` parent
edge** onto the `event` row it creates (the record lane, [events](/architecture/events/)),
and the CDC publisher re-emits both into the record-lane message header. So "carried on
NATS headers" is really header (data lane) -> PG column -> header (record lane): the walk
is unbroken because each hop copies the pair forward.

So no edge can close a loop: events come only from data, alarms are terminal toward
datapoints, the response layer cannot author events, operator transitions never
re-trigger, and the one real-world control loop is lineage-bounded.

### Correlation id: the trace of a causal chain

The same causation lineage the cycle guard walks also powers a read-side **trace**: a
**correlation id** that threads a whole causal chain end to end. A datapoint fires an
**event**, which opens an **alarm**, which triggers a **flow / action**, which runs a
**function / command**, which may change a value and clear the alarm. The `alarm_id` links
one alarm's own open / clear events; the **correlation id** links the *entire* chain, the
originating event through every downstream event and action it caused. It is built on the
existing causation lineage (an id stamped at the head and propagated along each caused
edge, riding **NATS message headers** across the bus), pure DX and observability sugar: it
lets an operator see the chain at a glance and query "everything this one event set in
motion."

The caused edge that crosses the device is carried explicitly: when an action runs a
command, the command **stamps its `correlation_id` onto the intended write and onto the
adaptive-poll's observed datapoint** ([datapoints](/architecture/datapoints/)), so the
chain stays threaded through the device round trip and the `event_rule` that fires off
the returned datapoint inherits the id.

It is **not** a datapoint kind and **not** a stored span subsystem, just an id carried on
the chain and queryable. No new tables, no tracing backend.

## Flows: the multi-step action

A **flow** is a bounded multi-step action: a **DAG of steps** (`notify`, `command`, `wait`, `branch`,
`parallel`) over one alarm. It is **instantiated per open alarm**, gated on the alarm staying open,
**cancelled on resolve or ack**, **cycle-guarded** by the same causation-lineage walk documented
above, and **finite** by a depth / step cap. It depends on the durable per-incident timer
([time](/architecture/time/)) for its `wait` steps.

The canonical case is an **escalation**: remediate, `wait`, re-check the **real datapoint** the alarm
is built on, and escalate if it is unchanged ("run the fix, wait 10m, if the datapoint still trips
page a human, wait 1h, page the next tier"). Two more cases fall out of the same engine: a **time-bound
access grant** (grant, `wait`, revoke) and an **AI-troubleshooting flow** that fetches more data, has
the model analyze it, and routes on the verdict.

This is the platform's programmable layer, and it is deliberately a **bounded** workflow engine, not a
Turing-complete one: a finite step list, lineage-guarded (above), with a depth / step cap as defense.
A flow does **not** author events; it acts, and any effect it has on the world returns as ordinary
data (which is the only edge that could re-open its alarm, and that edge is lineage-bounded). A
drag-and-drop editor edits flows.

**The single-step and multi-step shapes are one model.** An action is one or many steps: a
single `notify` or `command` is the simple case, and a multi-step **flow** is the same step list
grown past one over the same engine. Nothing in the design distinguishes them but the length of the
list.

## Namespacing

Event rules, actions, and severity levels carry the same **`official` boolean**
and `UpsertOfficial` as the rest of the registries. `official: true` rows ship
vetted; `official: false` rows are operator-authored and central to component templates (the
concrete way to notify or to run a command against a given device class).

## Storage

`alarm` and `action` are **stateful entities that hold their current state directly** (not event-sourced); the physical layout (the owner arc, partitioning) lives on [storage](/architecture/storage/).

| Table | Key columns | Notes |
|---|---|---|
| `alarm` | **id**, event_rule, owner arc, **status, severity, opened_at, resolved_at, acked_by** | a stateful entity, **one incident, new row per open**; holds current state directly (not event-sourced); the ITSM anchor. History = events + audit by `id` |
| `action` | id, **steps (ordered: notify/command/wait/branch)**, status, current_step | a stateful entity; delivery and step state; driven by events/alarms |

A command is **not a table**: it is a `component_template_version.spec` declaration (the interface `commands` block); a command instance is an `action` row with `kind=command`. The `event_rule` / `action_rule` config rows live with the [rule families](/architecture/calculations/).

## Model-keyed command cascade

An abstract action (`reboot`) resolves to different concrete commands by component type / model
through the cascade. The edge dispatch and the `command` declaration live in
[templates](/architecture/templates/) / [nodes](/architecture/nodes/); the cascade is the
abstract-to-concrete resolution layer above them, so one `reboot` action targets a heterogeneous
fleet and each device runs the command its model declares.

