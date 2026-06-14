---
title: Alarms and actions
description: How Hyperscale AV detects conditions with stateful alarms and responds with decoupled, cycle-safe actions and runbooks.
---

Component document of the
[architecture overview](/architecture/). The response half of the
pipeline: an **alarm** detects a condition and holds it; an **action** does
something about it. Both are **stateful entities that hold their state directly**
(not event-sourced). The credentials an action uses to reach a sink live in
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
```

`scope` selects the entities (fan-out, one alarm per match); `datapoint` is the
input; `window` / `for` are the aggregation machinery; `fire` / `clear` are the Expr
leaves; `severity` is the integer (below). A rule is **suppressible by name through
the cascade** ([cascade](/architecture/cascade/)): a high-weight group can remove a
false-firing rule without editing it (the firmware-bug workaround).

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
- a **multistep** action (a runbook) carries workflow state (current step, waiting,
  done), exactly like an alarm's lifecycle.

The `action` row carries identity, kind, config, and current status.

## The `action_rule` (decoupled subscription)

Detection and response are kept **separate**, the discipline that avoids Zabbix's
action/operation tangle: the `event_rule` does not contain its response. Instead an
`action_rule` **subscribes to events and alarms** via an Expr predicate, so one action
rule can serve many alarms. It is a subscription, not a fourth datapoint-pipeline
rule family (the three families, transform / calc / event, produce data; the
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

The `telemetry -> datapoint -> alarm` core is acyclic by construction (hub *Cycle
safety*). The action layer closes the remaining thread with three rules:

1. **`action_rule.source` admits an alarm transition (open / resolve, an `event`), a
   scheduled fire (an `event` with `origin=scheduled`), operator, and the declarative
   runbook step-list, never free-form `source=action`.** Action-to-action exists only
   as a bounded runbook.
2. **ack / snooze transitions do not match `action_rule`s** (only open / resolve do),
   which breaks the `action -> alarm(ack) -> action` loop.
3. **Runbooks are finite by construction** (a declarative step list, per-open-alarm,
   gated on open, cancelled on resolve / ack), with a depth / step bound as defense.

So no edge can close a loop: alarms are terminal toward datapoints, actions fire only
off open/resolve or bounded steps, and operator transitions never re-trigger.

## Runbook: the stateful meta-action (deferred, defined)

A **runbook** is a timer-driven, alarm-state-gated **sequence** of actions: "on this
alarm, check X, wait 10m, if still open run Y, wait 1h, escalate to a human." It is
an action that calls other actions over time, **instantiated per open alarm**, gated
on the alarm still being open, and **cancelled on resolve or ack**. It depends on a
durable per-incident timer (the time-source primitive, also deferred), so it lands
after leaf actions and time exist.

Hard line: it stays a **declarative step list, never a Turing-complete workflow**.
Complex logic lives inside a `run` action's script or a user-owned webhook receiver,
not in the platform (that way lies a workflow engine, a different product).
"Escalation policy" is the accurate concept; `runbook` is the working taxon (open to
`playbook` / `routine`). Naming caveat: in ops "runbook" also means a human-followed
documented procedure (likely a future KB artifact, a different surface); revisit if
that gets built.

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
- **Runbook** (above), behind leaf actions and the time-source primitive.

## Open items

- The runbook taxon (`runbook` / `playbook` / `routine`) and whether the human-doc
  sense collides.
- Whether `severity_levels` is purely a render lookup or also carries policy (e.g. a
  default ack-timeout per level).
- The dead-letter surface and operator replay of failed actions.
- Observed-use auth-failure feedback from actions into credential health
  (credentials open item).
