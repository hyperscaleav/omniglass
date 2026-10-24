---
title: "Logs are a raw ingest lane, not events"
date: 2026-07-28
status: "Accepted"
summary: "Logs are a raw ingest lane, not events"
---

- **Date:** 2026-07-28 | **Status:** Accepted

Pulling `log` out of `property_type` into `event_type` (#395) also promoted every raw log line, at
ingest, into a caught `log.line` event. Reviewing the seeded event types showed that conflates two
different things: a log line that **arrived** versus an occurrence that **happened**. It is the same
category slip as naming a type after its transport (the `syslog.line` to `log.line` fix): arrival is
not a happening.

**Three ingest shapes, not two.** A **property** (metric or state) is a sampled value with a current
value. An **event** is a semantic, typed happening. A **log line** is raw arrival: untyped text off a
firehose. A log line is neither of the other two, it is the substrate some events are derived from.

**Two lanes meet at `event`.** (1) The raw firehose lands in a `log_line` ingest table; derivation
rules read it and emit semantic `event` rows. (2) A component with a native event model (an xAPI
xEvent, an SNMP trap, a webhook) publishes straight to `event`. Neither lane is a subset of the other:
most log lines never become events (they are searched, retained, and aged out having produced
nothing), and native events never touch a log. That asymmetry is why `log_line` is its own table, not
a flag on `event`.

**`origin` names the producer.** `derived` (a rule produced it, from a log line or another event),
`caught` (a component published it natively), `scheduled`. The retired model's "a log line is a caught
event" collapses into: the log line is a `log_line` row, and if it matters a rule **derives** an event
from it.

**Lineage lives on the event row.** A derived event carries `source_event_id` (unified: the source is
the cause, so this replaces the separate `caused_by_event_id`), `source_log_line_id`, and
`derived_by_rule_id`, all null for a natively-caught event. The derivation engine keeps its own
execution history (which rule version fired, when, over what inputs), but that is a separate
observability lane and lineage must not depend on it: rotating the execution log cannot be allowed to
orphan an event's provenance. `derived_by_rule_id` is the bridge into that history when the full story
is wanted, without being the only home for the basic fact.

**A log line is untyped but classifiable by labels, not a registry.** There is deliberately no
`log_type` registry: the log lane's job is to swallow the firehose **without** pre-declaration, the
exact opposite of the reject-not-project contract that justifies the `property_type` and `event_type`
registries, and log classes (system, app, firmware, kernel) are device- and OS-specific and
open-ended. Classification is descriptive: a `source` channel plus freeform labels and `attributes`,
which an operator can extend without a schema change. The one exception is `severity` (and a coarse
`facility`), promoted to indexed columns because retention and routing policy keys on them (keep
firmware and error lines longer, drop debug faster). The shape is
`log_line { ts, owner, source, severity?, facility?, message, attributes, labels, correlation_id }`.

**Build gate.** The `log_line` table, retention, the derivation engine, the lineage columns, and
native-event producers are their own slice ([#410](https://github.com/hyperscaleav/omniglass/issues/410)),
worth building when logs are a real firehose to search and age out separately. This ADR records the
target; it is not built all at once.

**Consequence for #395 (the event_type family slice).** The bits that encode the retired model come
out now: the ingest log-to-event promotion and the seeded `log.line` event type. The slice keeps the
`event_type` registry, the richer `event` (origin, causation, correlation), `log` leaving
`property_type`, and a native caught example (`call.started`). The `caused_by_event_id` to
`source_event_id` rename and the new lineage columns land together in the log-lane slice, not
piecemeal here.
