---
title: Events
description: "Events: our semantic assertion that something happened, the event_type registry, and the four ways an event arrives."
sidebar:
  badge:
    text: Design
    variant: caution
---

An **event** is *our semantic assertion that something happened*, in our vocabulary: a discrete, point-in-time occurrence the action layer reacts to, owned through the same exclusive-arc as a datapoint. It is **not** a datapoint (a datapoint records a value; an event records an occurrence, see [the has-a-value-now razor](/architecture/datapoints/#the-has-a-value-now-razor-datapoint-vs-event)). Datapoints are what rules read; events are what event rules produce. The rules that produce events live on [calculations](/architecture/calculations/); the alarms paired events drive, and the actions that respond, live on [alarms and actions](/architecture/alarms-actions/).

## The event_type registry

A datapoint and an event are different shapes (a datapoint has a value; an event is an occurrence), so each gets a registry named for what it holds. The datapoint half is [`datapoint_type`](/architecture/datapoints/#the-datapoint_type-registry); the event half is `event_type`. We do **not** force them into one universal registry, that would be the false unification the rest of the model avoids.

**`event_type`** describes every event key: `(name, display_name, payload_schema, scope, ...)`, with the same **`scope`** (template / org / official) as the datapoint registry; a template can define a template-local event. Declaring event types (`call.started`, `cable.unplugged`, `command.sent`) is first-class and valuable: it gives events a known schema, makes them inspectable, and is what lets an event rule promote a raw log line into a *registered* event. An event key is registered here; an unregistered occurrence stays a `log_datapoint` line until a rule promotes it.

The naming convention is consistent: a `_type` registry defines what a thing *is*, named for the thing (`datapoint_type`, `event_type`, like `component_type`, `interface_type`). Events get their own registry because an event is a different shape from a datapoint. The `scope` axis works the same way as for datapoints: see [key scope](/architecture/datapoints/#key-scope-template-org-official).

## Events: caught, caused, derived, scheduled

An event arrives one of four ways; none is auto-manufactured from a state flip (a transition is already two consecutive datapoint rows, derivable by query).

1. **caught**: a structured occurrence arrives (xAPI Event channel, a webhook, a trap), or an event rule **promotes** a `log_datapoint` line into a normalized event.
2. **caused**: we issued a command, recorded as an event; this is what opens an [intended](/architecture/datapoints/#intended-the-declared-effect-of-a-command) datapoint.
3. **derived**: an event rule fuses signals into an operator-meaningful fact ("codec in-call + traffic spike + room booked, so meeting started"), inferred without instrumenting the control system.
4. **scheduled**: the clock fired a schedule. A schedule fire *is* an event with `origin=scheduled`, manufactured by the clock (a leader-elected singleton held via a NATS KV CAS lock, exactly one active, failing over on death); there is no separate schedule log table. So `action_rule` subscribes to events uniformly (**schedule to event to action**: digests, synthetic checks, SLA resets are all schedule fires an action subscribes to).

Caught/caused/derived/scheduled is the event's **origin**, a small vocabulary on the event table; it is not the same enum as datapoint provenance. The discipline that keeps an event-driven system from rotting is that events are declared (registered event keys) and rules are inspectable (the blast-radius preview in the UI).

## Storage

The `event` row is the semantic-occurrence log; `event_type` is its key registry. The physical layout (partitioning, the owner arc, lineage) lives on [storage](/architecture/storage/).

An event is **born in a Postgres transaction**, on the record lane. When an `event_rule` fires, the consumer writes the `event` row and its paired alarm transition to PG in one transaction (the alarm edge is serialized per `(event_rule, owner)`); the event is the durable record, the alarm is the stateful edge. The event is **not** published directly from the rule (no dual-write): a leader-elected CDC publisher (logical decoding of the WAL) fans the committed change out to JetStream, where the `action_rule` consumers react. Postgres is the system of record; JetStream carries the committed event onward. This is the opposite lane from datapoints, which live on NATS and sink to PG asynchronously (see [datapoints](/architecture/datapoints/)).

| Table | Key columns | Notes |
|---|---|---|
| `event` | id, ts, key, **origin** (caught/caused/derived/scheduled), owner arc, payload (jsonb), correlation_id, **caused_by_event_id** (nullable), **alarm_id** (nullable), + lineage | the semantic-occurrence log; a momentary event has null `alarm_id`, an alarm edge carries it; `caused_by_event_id` is the parent edge so the cycle guard can walk causation upstream (the flat `correlation_id` threads the chain, this points at the parent). A schedule fire is an event with `origin=scheduled` (no separate schedule table) |
| `event_type` | name, display_name, **payload_schema (jsonb)**, **scope** | the event-key registry; lets an event_rule promote a raw log line into a registered event. `scope` (template / org / official) works the same way as `datapoint_type` |

Related: [calculations](/architecture/calculations/) (the `event_rule` that produces events), [alarms and actions](/architecture/alarms-actions/) (alarms and the response layer), [datapoints](/architecture/datapoints/) (the data events read), and [the glossary](/architecture/glossary/).
