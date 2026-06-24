---
title: Events
description: "Events: our semantic assertion that something happened, the event_type registry, and the four ways an event arrives."
sidebar:
  badge:
    text: Spec
    variant: caution
---

An **event** is *our semantic assertion that something happened*, in our vocabulary: a discrete, point-in-time occurrence the action layer reacts to, owned through the same exclusive-arc as a datapoint. It is **not** a datapoint (a datapoint records a value; an event records an occurrence, see [the has-a-value-now razor](/architecture/datapoints/#the-has-a-value-now-razor-datapoint-vs-event)). Datapoints are what rules read; events are what event rules produce. The rules that produce events live on [calculations](/architecture/calculations/); the alarms paired events drive, and the actions that respond, live on [alarms and actions](/architecture/alarms-actions/).

## The event_type registry

A datapoint and an event are different shapes (a datapoint has a value; an event is an occurrence), so each gets a registry named for what it holds. The datapoint half is [`datapoint_type`](/architecture/datapoints/#the-datapoint_type-registry); the event half is `event_type`. We do **not** force them into one universal registry, that would be the false unification the rest of the model avoids.

**`event_type`** describes every event key: `(name, display_name, payload_schema, official, ...)`, with the same **`official` boolean** as the datapoint registry marking shipped-canonical versus org-local rows. Declaring event types (`call.started`, `cable.unplugged`, `command.sent`) is first-class and valuable: it gives events a known schema, makes them inspectable, and is what lets an event rule promote a raw log line into a *registered* event. An event key is registered here; an unregistered occurrence stays a `log_datapoint` line until a rule promotes it.

The naming convention is consistent: a `_type` registry defines what a thing *is*, named for the thing (`datapoint_type`, `event_type`, like `component_type`, `interface_type`). Events get their own registry because an event is a different shape from a datapoint. The `official` boolean works the same way as for datapoints: see [the `official` boolean](/architecture/datapoints/#the-official-boolean-shipped-canonical-versus-org-local).

## Events: caught, caused, derived, scheduled

An event arrives one of four ways; none is auto-manufactured from a state flip (a transition is already two consecutive datapoint rows, derivable by query).

1. **caught**: a structured occurrence arrives (xAPI Event channel, a webhook, a trap), or an event rule **promotes** a `log_datapoint` line into a normalized event.
2. **caused**: we issued a command, recorded as an event; this is what opens an [intended](/architecture/datapoints/#intended-the-declared-effect-of-a-command) datapoint.
3. **derived**: an event rule fuses signals into an operator-meaningful fact ("codec in-call + traffic spike + room booked, so meeting started"), inferred without instrumenting the control system.
4. **scheduled**: the clock fired a schedule. A schedule fire *is* an event with `origin=scheduled`, manufactured by the clock worker; there is no separate schedule log table. So `action_rule` subscribes to events uniformly (**schedule to event to action**: digests, synthetic checks, SLA resets are all schedule fires an action subscribes to).

Caught/caused/derived/scheduled is the event's **origin**, a small vocabulary on the event table; it is not the same enum as datapoint provenance. The discipline that keeps an event-driven system from rotting is that events are declared (registered event keys) and rules are inspectable (the blast-radius preview in the UI).

## Storage

The `event` row is the semantic-occurrence log; `event_type` is its key registry. The physical layout (partitioning, the owner arc, lineage) lives on [storage](/architecture/storage/).

| Table | Key columns | Notes |
|---|---|---|
| `event` | id, ts, key, **origin** (caught/caused/derived/scheduled), owner arc, payload (jsonb), correlation_id, **alarm_id** (nullable), + lineage | the semantic-occurrence log; a momentary event has null `alarm_id`, an alarm edge carries it. A schedule fire is an event with `origin=scheduled` (no separate schedule table) |
| `event_type` | name, display_name, **payload_schema (jsonb)**, **official** | the event-key registry; lets an event_rule promote a raw log line into a registered event. The `official` boolean marks shipped-canonical versus org-local |

Related: [calculations](/architecture/calculations/) (the `event_rule` that produces events), [alarms and actions](/architecture/alarms-actions/) (alarms and the response layer), [datapoints](/architecture/datapoints/) (the data events read), and [the glossary](/architecture/glossary/).
