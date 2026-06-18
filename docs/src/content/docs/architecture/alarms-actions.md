---
title: Alarms and actions
description: How Omniglass detects a condition with a stateful alarm and responds with an action, which is a flow when it has more than one step.
---

Component document of the
[architecture overview](/architecture/). The response half of the
pipeline: an **alarm** detects a condition and holds it; an **action** does
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
yields a system/location-owned alarm), the ITSM correlation anchor ([taxonomy](/architecture/taxonomy/)).
The open and resolve **events** carry the `alarm_id` and are the edge log; the alarm
row is the live state.

Transitions, by who drives them:

- **opened / resolved** are **rule-driven** (the `event_rule`'s fire / clear
  criterion), each emitting an `event` carrying the `alarm_id`;
- **acked / snoozed** are **operator-driven**, recorded in `audit_log` (also carrying
  the `alarm_id`) and applied to the alarm row.

The full timeline assembles by `alarm_id` across events + audit; the alarm row is
never reconstructed from them.

Alarms are **terminal upstream**: they never write datapoints, so they cannot feed
back into the datapoint layer (hub *Cycle safety*).

## The `event_rule`

An `event_rule` carries a required `fire` criterion and an optional `clear`
criterion. With a `clear` criterion the fire event **opens** an alarm and the clear
event **resolves** it; without one the rule is momentary (a one-shot event, no
alarm). There is no separate `alarm_rule`.

```yaml
event_rule:
  scope: 'component.template == "polaris-dsp-16"'   # the shared selector (expressions)
  datapoint: dsp.temperature
  window: { reduce: avg, over: 10m }                 # machinery (optional)
  fire: "value > 65"                                 # opens the alarm
  clear: "value < 60"                                # resolves it (defaults to !fire)
  for: 0                                             # sustained span (optional)
  severity: 30
  health: degraded                                   # optional: degrade the owner's health while open
```

`scope` selects the entities (fan-out, one alarm per match); `datapoint` is the
input; `window` / `for` are the aggregation machinery; `fire` / `clear` are the Expr
leaves; `severity` is the integer (below). A rule is **suppressible by name through
the cascade** ([cascade](/architecture/cascade/)): a high-weight group can remove a
false-firing rule without editing it (the firmware-bug workaround).

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

## Severity: an open integer, not an enum

The alarm carries a **severity integer (0-999)**. A **`severity_levels` lookup**
(the `configuration` store, official defaults) renders the integer to a **label +
color**; the integer is the sortable, comparable value. Official defaults are
**spaced by 10** so operators can insert without renumbering:

| value | label | color |
|---|---|---|
| 10 | info | gray |
| 20 | warning | yellow |
| 30 | average | orange |
| 40 | high | red |
| 50 | disaster | dark red |

Higher is more severe. An operator can relabel to P1/P2/P3, define three levels or
seven, or slot a 25 between warning and average, all config, no code. Rules and
`action_rule` predicates compare the integer (`alarm.severity >= 40`); the UI renders
via the lookup.

## The action (a stateful entity)

What an `action_rule` raises and runs. Like an alarm it is **stateful** and holds
its own state directly (status, current step, delivery), not event-sourced:

- **kinds**: `notify` (in-app), `webhook`, `email`, `run` (execute a command; the
  edge realization is in [components](/architecture/components/) / [nodes](/architecture/nodes/)).
- a **simple** action carries delivery state (`queued / sent / failed / retried`,
  the at-least-once outbox);
- a **multistep** action is a **flow**: it carries workflow state (current step, waiting,
  branches), exactly like an alarm's lifecycle.

The `action` row carries identity, kind, config, and current status.

## The `action_rule` (decoupled subscription)

Detection and response are kept **separate**, the discipline that avoids Zabbix's
action/operation tangle: the `event_rule` does not contain its response. Instead an
`action_rule` **subscribes to events and alarms** via an Expr predicate, so one action
rule can serve many alarms. Subscriptions are **indexed by event key and label**, so dispatch
evaluates only the predicates whose key or label already matches, not every rule on every event;
an action rule may carry **multiple triggers** and fires if any matches (including a label or
wildcard trigger, e.g. any event labeled `room=boardroom-a`). It is a subscription, not a fourth datapoint-pipeline
rule family (the derivation rules, calc and event, produce data; the
`action_rule` wires the resulting events and alarms to actions):

```yaml
action_rule:
  on: alarm
  when: 'transition == "open" && alarm.severity >= 40 && component.type == "device"'
  action: pagerduty-notify
```

The **source is polymorphic** but guarded (see *Cycle safety*): an alarm transition
(open / resolve, each an `event` carrying the `alarm_id`), a scheduled fire (an
`event` with `origin=scheduled`, not a separate source), an operator (manual), and
the declarative runbook step-list. Bodies are **Go templates** and sink auth is a
**credential reference**, both per [expressions](/architecture/expressions/) and
credentials.

## Durability and egress

A transactional **outbox** row is written with the action's step transition; an
**outbox relay** drains it (`SELECT ... FOR UPDATE SKIP LOCKED`, retry with
backoff, dead-letter), the same pattern as the rule engine's work queue
(workers). External sends are **at-least-once**; sinks tolerate
dupes or we add an **idempotency key** (alarm + action + transition). Pipeline order:
**render the body, then apply auth over the rendered bytes** (HMAC signs the rendered
body), then send. **Egress safety** is always on: block internal / metadata IPs,
verify TLS, bound timeouts, control redirects.

## Cycle safety in the action layer

The `collection -> datapoint -> alarm` core is acyclic by construction (hub *Cycle
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
   an action, the engine walks the triggering event's **causation lineage**; if the
   same `(action, owner)` already appears upstream it is suppressed, with a depth bound
   as a backstop. Flows are finite by construction (a step list, per-open-alarm, gated
   on open, cancelled on resolve / ack).

So no edge can close a loop: events come only from data, alarms are terminal toward
datapoints, the response layer cannot author events, operator transitions never
re-trigger, and the one real-world control loop is lineage-bounded.

## Flows: the multi-step action (deferred, defined)

A **flow** is a multi-step action: a DAG of steps that can branch, run in parallel, and wait. The
canonical case is an **escalation**: "on this alarm, check X, wait 10m, if still open run Y, wait
1h, page a human." A flow is **instantiated per open alarm**, gated on the alarm still being open,
and **cancelled on resolve or ack**. It depends on a durable per-incident timer
([time](/architecture/time/)), so it lands after the leaf step kinds and time exist.

This is the platform's programmable layer: branching, parallel steps, `wait`, and acting (`notify`,
`command` a device, page a human). A flow does **not** author events; it acts, and any effect it has
on the world returns as ordinary data. It is a **bounded** workflow engine, not a Turing-complete
one: finite steps, lineage-guarded (above), with a depth/step cap as defense. That is enough for the
real cases, an escalation, a time-bound access grant, an AI-troubleshooting flow that fetches more
data and analyzes it, while staying safe to run. A future drag-and-drop editor edits flows.

## Namespacing

Event rules, actions, and severity levels get the same **official / private**
namespacing and `UpsertOfficial` as the rest of the registries. Official ships
vetted; private is operator-authored and central to component templates (the
concrete way to notify or to run a command against a given device class).

## Deferred

- **Model-keyed command cascade**: an abstract action (`reboot`)
  resolving to different concrete commands by component type / model through the
  cascade (the edge dispatch and the `command` declaration already exist in
  [components](/architecture/components/) / [nodes](/architecture/nodes/); this is the abstract-to-concrete resolution layer above them).
- **Flows** (above), the multi-step action, behind the leaf step kinds and the time primitive.

## Open items

- The depth bound and lineage-detector shape for the data-mediated control loop (an action whose
  effect re-opens its own alarm).
- Whether `severity_levels` is purely a render lookup or also carries policy (e.g. a
  default ack-timeout per level).
- The dead-letter surface and operator replay of failed actions.
- Observed-use auth-failure feedback from actions into credential health
  (credentials open item).
