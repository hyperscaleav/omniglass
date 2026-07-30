---
title: Events
description: "Events: our semantic assertion that something happened, the event_type registry, and the four ways an event arrives."
sidebar:
  badge:
    text: Partial
    variant: note
---

:::note[Partial: the event_type registry and the richer occurrence are built]
[ADR-0063](/architecture/decisions/#adr-0063-the-telemetry-model-is-typed-registries-over-bare-noun-data-tables) confirms the separation this page describes. The **`event_type` registry** is built: list, get, and custom-type create/update/delete over `/event-types`, seeded official types (`call.started`, `command.issued`), operator-extensible, official rows read-only, mirroring the `property_type` catalog. The **`event`** occurrence now carries **`origin`** (caught/caused/derived/scheduled) plus the ADR-0066 lineage columns (`source_event_id`, `source_log_line_id`, `derived_by_rule_id`) and the `correlation_id` trace, and two producers are built. The **native caught path**: a component that publishes an event (an xAPI event, an SNMP trap) has its registered `event_type` name route it to the event sink with `origin=caught`, under the same owner-confinement and reject-not-project as a metric or state. The **`caused`** origin: a [command](/architecture/commands/) records a caused `command.issued` event on issue (#396). Raw log lines are a **separate ingest lane**, not events ([ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events)): the `log_line` table, its per-component and per-node reads, and the first **producer** are built. A node ships its own operational log lines (connected to the bus, worklist pulled, a task skipped) off the telemetry ingest lane as untyped `LogLine`s; the ingest consumer lands them in `log_line` owner-bound to the node (a console **Self-logs** panel on the node blade, beside the component Logs one). The lineage columns are in place (a derived event will carry `source_log_line_id` and `derived_by_rule_id`), while the **derivation rules** that turn a log line into a registered event are directional, along with the `event_rule` engine and the `derived`/`scheduled` producers, the alarm edge, and the CDC publisher.
:::

An **event** is *our semantic assertion that something happened*, in our vocabulary: a discrete, point-in-time occurrence the action layer reacts to, owned through the same exclusive-arc as a sample. It is **not** a sample (a sample records a value; an event records an occurrence, see [the has-a-value-now razor](/architecture/properties/#the-has-a-value-now-razor-sample-vs-event)). Samples are what rules read; events are what event rules produce. The rules that produce events live on [calculations](/architecture/calculations/); the alarms paired events drive, and the actions that respond, live on [alarms and actions](/architecture/alarms-actions/).

## The event_type registry

A sample and an event are different shapes (a sample has a value; an event is an occurrence), so each gets a registry named for what it holds. The sample half is [`property_type`](/architecture/properties/#the-property_type-registry); the event half is `event_type`. We do **not** force them into one universal registry, that would be the false unification the rest of the model avoids.

**`event_type`** describes every event key: `(name, display_name, payload_schema, official, ...)`. The built registry carries the **`official`** boolean (shipped-canonical versus org-local), like the sample registry; the fuller **scope ladder** (template / org / official, where a template can define a template-local event) is future design. Declaring event types (`call.started`, `cable.unplugged`, `command.issued`) is first-class and valuable: it gives events a known schema, makes them inspectable, and is what a derivation rule targets when it turns a raw log line into a *registered* event. Raw log lines live in a [separate ingest lane](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events) (the `log_line` table), untyped, until a rule derives a registered event from one; most never become events at all.

The naming convention is consistent: a `_type` registry defines what a thing *is*, named for the thing (`property_type`, `event_type`, like `location_type`, `interface_type`). Events get their own registry because an event is a different shape from a sample. The designed `scope` axis would work the same way as for samples: see [key scope](/architecture/properties/#key-scope-template-org-official).

## Events: caught, caused, derived, scheduled

An event arrives one of four ways; none is auto-manufactured from a state flip (a transition is already two consecutive sample rows, derivable by query).

1. **caught**: a component publishes a structured occurrence natively (an xAPI Event channel, a webhook, an SNMP trap).
2. **caused**: we issued a command, recorded as an event; this is what opens an [intended](/architecture/properties/#intended-the-declared-effect-of-a-command) sample.
3. **derived**: an event rule fuses signals into an operator-meaningful fact ("codec in-call + traffic spike + room booked, so meeting started"), or turns a raw log line from the [`log_line` lane](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events) into a registered event, inferred without instrumenting the control system.
4. **scheduled**: the clock fired a schedule. A schedule fire *is* an event with `origin=scheduled`, manufactured by the clock (a leader-elected singleton held via a NATS KV CAS lock, exactly one active, failing over on death); there is no separate schedule log table. So `action_rule` subscribes to events uniformly (**schedule to event to action**: digests, synthetic checks, SLA resets are all schedule fires an action subscribes to).

Caught/caused/derived/scheduled is the event's **origin**, a small vocabulary on the event table; it is not the same enum as sample provenance. The discipline that keeps an event-driven system from rotting is that events are declared (registered event keys) and rules are inspectable (the blast-radius preview in the UI).

## Storage

The `event` row is the semantic-occurrence log; `event_type` is its key registry. The physical layout (partitioning, the owner arc, lineage) lives on [storage](/architecture/storage/).

An event is **born in a Postgres transaction**, on the record lane. When an `event_rule` fires, the consumer writes the `event` row and its paired alarm transition to PG in one transaction (the alarm edge is serialized per `(event_rule, owner)`); the event is the durable record, the alarm is the stateful edge. The event is **not** published directly from the rule (no dual-write): a leader-elected CDC publisher (logical decoding of the WAL) fans the committed change out to JetStream, where the `action_rule` consumers react. Postgres is the system of record; JetStream carries the committed event onward. This is the opposite lane from samples, which live on NATS and sink to PG asynchronously (see [samples](/architecture/properties/)).

| Table | Key columns | Notes |
|---|---|---|
| `event` | id, ts, **event_type_id** (NOT NULL FK to `event_type`), **origin** (caught/caused/derived/scheduled), owner arc, **instance**, **message**, **attributes** (jsonb), **provenance**, **source**, correlation_id, the lineage columns **`source_event_id`** / **`source_log_line_id`** / **`derived_by_rule_id`** (all nullable) | the semantic-occurrence log; there is no `alarm_id` column today, the event-carries-its-alarm edge is design direction. The lineage (ADR-0066) names what produced the event: `source_event_id` the parent event, `source_log_line_id` the raw log line a rule derived it from, `derived_by_rule_id` the rule (a natively-caught event has none), while the flat `correlation_id` threads the causal chain. A schedule fire is an event with `origin=scheduled` (no separate schedule table) |
| `event_type` | name, display_name, **payload_schema (jsonb)**, **official** | the event-key registry; what a derivation rule targets when turning a raw log line (in the separate `log_line` lane) into a registered event. The built column is the `official` boolean (shipped-canonical versus org-local); the `scope` ladder (template / org / official) is future design, shared with `property_type` |

Related: [calculations](/architecture/calculations/) (the `event_rule` that produces events), [alarms and actions](/architecture/alarms-actions/) (alarms and the response layer), [samples](/architecture/properties/) (the data events read), and [the glossary](/architecture/glossary/).
