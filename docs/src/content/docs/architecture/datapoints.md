---
title: Datapoints
description: "The core data model: datapoints and their three kinds, provenance, the registries, key scope, divergence, fusion, and how a value reads back."
sidebar:
  badge:
    text: Partial
    variant: note
---

:::note[Partial]
The observed **metric** path is built for reachability: a node's tcp probe produces `tcp.open` /
`tcp.connect_time` and its icmp (ping) probe produces `icmp.reachable` / `icmp.rtt_avg`, and the ingest consumer writes them to `metric_datapoint` with `provenance=observed`, the
owner bound server-side from the task's interface (`owner_kind=component`) and reject-not-project enforced
against the `datapoint_type` registry. The observed **state** path now has its first producer too: the node
computes the per-interface reachability verdict `interface.reachable` (up/down, the AND of the interface's probe
results), and the ingest consumer **routes by the `datapoint_type` kind** (metric to `metric_datapoint`, state to
`state_datapoint`), so the verdict lands in `state_datapoint` under the same owner-confinement, **transition-only**
(one row per flip, guarded at the node and again at ingest). Log datapoints, calculated provenance, fusion, and
the live NATS data-lane described below are still design. See
[ADR-0033](/architecture/decisions/#adr-0037-telemetry-is-a-protobuf-event-over-jetstream-with-an-inline-owner-confining-consumer)
and [ADR-0034](/architecture/decisions/#adr-0038-the-reachability-verdict-is-a-built-in-state).
:::

This is the heart of the authoritative data model: what a datapoint is, the two axes that define it, how we know a value (provenance), and how values reconcile, diverge, and read back. The physical layout (tables, partitioning, the lineage CHECK, tiering) lives in storage; the spine is [the architecture overview](/architecture/). Events, calc rules, and the response layer get their own pages: [events](/architecture/events/), [calculations](/architecture/calculations/), and [alarms and actions](/architecture/alarms-actions/).

Datapoints are the **data lane**: observed and calculated datapoints are NATS-native, published to a JetStream `datapoints` stream and consumed live by the rule engine, with a persistence consumer batch-writing them to the PG tables as an async sink. Datapoints are the firehose and never wait on Postgres. Events, alarms, and actions are the **record/state lane**: born in a PG transaction and fanned out by change-data-capture (CDC). The two lanes share one bus (JetStream); this page is home for the data lane, and points at [events](/architecture/events/) and [alarms and actions](/architecture/alarms-actions/) for the record lane.

## Datapoints: one family, three kinds

A **datapoint** is an observation: a value of one key, on one owning entity (component, system, or location), at one time. The row shape is the same for all three kinds: `(owner, key, instance, ts, value, provenance, source, lineage)`. They are three physical tables only because they index and retain differently, not because they are different concepts.

- **metric** (`metric_datapoint`): numeric (float), carries a **unit**. Continuous, aggregatable. Has a current value.
- **state** (`state_datapoint`): categorical, text, or a structured object. Discrete, dwell-measurable. Has a current value (`last()` is meaningful).
- **log** (`log_datapoint`): a component's own words, the value is the log line (text or jsonb), keyed by log type (`log.system`, `log.os`, `log.app.<name>`). A stream, not a current value, but still an observation with a value at a time, so it is a datapoint, not a separate primitive. In practice only components emit logs.

Treating log as a datapoint removes the usual special case: an alarm on a log line is just an event rule whose condition matches a `log_datapoint` value, no different in shape from a metric threshold.

**An event is not a datapoint.** A datapoint is an observation (a value we recorded); an **event** is *our semantic assertion that something happened*, in our vocabulary. Datapoints are what rules read; events are what event rules produce. See [events](/architecture/events/).

### Ownership: the exclusive-arc

A datapoint attaches to a **structural entity**, not only a component. The owner is the **exclusive-arc**: an `owner_kind` enum plus the matching typed FK (`component_id` / `system_id` / `location_id` / `node_id`, or none for the singleton **`global`** estate root) with a CHECK that exactly the column matching `owner_kind` is set. The same arc owns `event` and `alarm` rows. This makes **system-, location-, node-, and global-level datapoints first-class** (e.g. `health` is a `state_datapoint` owned by a system, and estate-wide availability is owned by `global`), the fix for Zabbix's inability to put state on a group of hosts. See Ownership on the spine for the full pattern and the storage DDL.

### The instance dimension: many values of one key on one owner

One owner can hold several distinct values of the *same* canonical key: three fan speeds on a switch, per-port counters, per-channel audio levels. The canonical registry deliberately holds **one** `datapoint_type` per measurement (`fan.speed`, not `fan.speed.intake`), so the discriminator lives outside the key, as an `instance text NOT NULL DEFAULT ''` column on all three datapoint tables. Series identity is therefore **`(owner, datapoint_type, instance, provenance)`**: each instance is its own series, while a singleton (`instance = ''`) is the default. Aggregation stays clean (group by `key`, ignore `instance`); per-instance trends stay distinct.

The instance rides the pipeline as a reserved **`instance` label** on the collected datapoint: the collection extract spec authors it as a `key[instance]` suffix (`fan.speed[intake]=<oid>`, `fan.speed[exhaust]=<oid2>`), the parser strips the bracket into the label so `registryAllows` / `kindFor` still match the bare canonical key, and the derive step reads `instance` into the column. Calc folds **every** instance of an input key into the reduce: a rule reading `fan.speed` from a component gets one candidate per fan, so `worst` / `average` / `count` / Expr aggregate across all of them (a singleton key yields one candidate). An input filter can select one instance (`instance == "intake"`). Recompute needs no instance granularity: a calc consumer reacting to a `fan.speed` datapoint on the stream recomputes over the current state of every instance for `(owner, key)`, so two fan changes in close succession converge on one correct recompute. Calc **outputs** stay aggregate (`instance = ''`); per-instance outputs (one health per fan, a group-by) are a separate capability, not a silent gap, output owners default to the singleton.

### The has-a-value-now razor (datapoint vs event)

A datapoint records a value; an event records an occurrence.

- `"input is 1"` is a value, so it is a **datapoint** (state).
- `"call started"` is an occurrence, "what is call-started now?" is meaningless, so it is an **event**. See [events](/architecture/events/).

A raw occurrence we have not normalized (a syslog line, a raw webhook frame) lands as a **`log_datapoint`** (observed, value = the line). An event rule can then **promote** it into a normalized event. So the log table is also the holding pen for un-normalized occurrences until a rule recognizes them.

## Kind and provenance: the two axes

Every datapoint sits on two independent axes:

- **Kind** answers *what kind of thing is this?* It is fixed per **key**, decided once when the key is defined (`power.state` is always a state), so kind is a property of the **key**, the three kinds above.
- **Provenance** answers *how do we know this particular value?* It varies per **row**: the same `power.state` can be observed or intended at different moments. (A *declared* desired value is not a provenance; it lives in [config](/architecture/variables/), keyed to the signal.) Provenance is a property of the **row**, detailed below.

Kind is set by the key, provenance by the row, and the two never depend on each other.

## The datapoint_type registry

A datapoint and an event are different shapes (a datapoint has a value; an event is an occurrence), so each gets a registry named for what it holds. The event half is [`event_type`](/architecture/events/); the datapoint half is `datapoint_type`. We do **not** force them into one universal registry, that would be the false unification the rest of this model avoids.

**`datapoint_type`** describes every datapoint key: `(name, scope, template_id?, kind, value_type, unit, precision, fusion_policy, validation)`, with the **`scope`** (`template` / `org` / `official`) deciding where the name is unique (see [Key scope](#key-scope-template-org-official)). One registry across all three datapoint kinds (metric/state/log). The kind is decided by the key, not at runtime: the compiler bakes each key's kind into the edge unit, so a value routes to the right table with no runtime lookup, the same way at every scope. `fusion_policy` is the key's read-time **default** for reducing multiple perspectives, a hint rather than a mandate (see [Fusion](#fusion)). A key names a **measurement, never its owner** (`temperature`, not `room.temperature`), with snake_case segments in a dot hierarchy and the **canonical unit** in the `unit` field (`fan.speed` + `unit: rpm`, not `fan_rpm`); the ship-with official set lives in `internal/registry/defaults.yaml`. Adding or naming one: the `canonical-datapoint` skill.

The naming convention is consistent: a `_type` registry defines what a thing *is*, named for the thing (`datapoint_type`, `event_type`, like `component_type`, `interface_type`). `datapoint_type` spans the three datapoint kinds, and events get their own registry because an event is a different shape.

**Datapoint key naming is owner-agnostic.** A key names a *measurement*, never its owner: `temperature` is a Celsius reading whether a codec's thermals or a room's ambient sensor produced it, and the owner (component / system / location / node / global) plus a template's labels and the function that collected it give it context. So there is no `system.` / `device.` / `room.` prefix; keys group by measurement domain (`cpu.utilization`, `power.state`, `video.input`, `audio.level`, `network.icmp.rtt`). This is the normalization the product hinges on: one canonical path means one comparable signal across every vendor, which is what makes cross-fleet dashboards and AI useful. The official set is seeded from `internal/registry/defaults.yaml` following OpenTelemetry semantic conventions for the IT leaves (`cpu.utilization`, `memory.usage`; semconv's own `system.` prefix is dropped to avoid colliding with the `system` entity type) and the [OpenAV minimum-device-functionality guidelines](https://github.com/OpenAVCloud/specifications/blob/main/min-device-functionality/OAVC-AV-Device-Minimum-Functionality-Guidelines.md) for AV signals. A template declares its datapoints at **template** scope, or references an **org** or **official** key: the distro mints **official** keys, the deployment mints **org** keys, and a template mints its own **template**-scoped keys (the Zabbix model, where two templates can both declare an `input` with no collision).

### One identity, three shapes

`datapoint_type` is **one registry, not three**. A key's **kind** is intrinsic and fixed (one key is one kind, forever), so identity, [scope](#key-scope-template-org-official), and the promotion ladder live on one row, and `(scope, name)` is unique across all kinds: a name is a metric or a state, never both. What differs by kind is the **shape** the row carries:

- **metric**: `value_type` (float), a **unit** and optional **precision**, and a numeric range (`validation: {min,max}`). The full numeric shape.
- **state**: a **value domain**, the allowed set (`validation: {values:[...]}`); no unit, no precision.
- **log**: almost nothing. There is **no `log_type`** worth the name: a log's "type" is its **key namespace** (`log.system`, `log.os`, `log.app.<name>`, the hardware / service families), plus a level. You never give it a unit, a domain, or fusion.

These shapes ride **inline** on the one row today: kind-conditional columns (unit / precision, on metrics) and the `validation` jsonb that reads as a range for a metric and a domain for a state. The kind is decided once and compiled into the edge unit, and the registry is cached in-process, so reading a type's shape is a map lookup, never a per-datapoint join.

If the metric and state shapes grow, they may later move to **1:1 per-kind sidecar tables** (`metric_type`, `state_type`) keyed by the `datapoint_type` id, exactly as the IAM [`principal`](/architecture/identity-access/) splits into per-kind `human` / `service` / `node` tables (`log` keeps no sidecar). That is a cold-path normalization, one cheap PK join when reading the shape, and the registry is cached either way, not a hot-path change: the firehose never joins the type registry.

**Validation on insert, under a policy.** Every datapoint is typed by a `datapoint_type` row (the FK is **non-null**: a template-scoped key is a real `datapoint_type` row at `scope=template`, not an inline-only shape), so insert checks two things, plus an optional third. **(1) The key resolves** to a `datapoint_type` at a reachable scope: a template-scoped key self-resolves; a referenced org or official key must exist, checked at template compile time. An unresolved key is **reject-not-project**: kept out of the typed series, a `datapoint.validation_failed` event raised, the raw carried on a `collection.failed` event so nothing is lost (backfillable once the registry or template is corrected). **(2) The value conforms** to the type's kind and domain: the type's `validation` (`{min,max}` for a metric, `{values:[...]}` for a state). Optionally **(3) the owner kind** must be one the type allows. All three are governed by the `validation_policy` config mode: **bypass** (skip), **audit** (the default: write the value but emit the event), or **enforce** (hold the value back from the typed series, emit the event). The point is visibility: an out-of-range or unmapped value means a template author declared a type the device disagrees with, so the violation surfaces as an owner-attributed event operators and admins see. The mode resolves per-entity down the cascade (global, location, system, component), so a noisy device class can run in `audit` while the rest of the fleet enforces.

## Units: one canonical unit per key

**Unit is a metric concept.** Only a **metric** carries a unit (and the display `precision` below): a number needs a unit to mean anything, and even a dimensionless metric has one (`ratio` or `count`). A **state** (categorical) and a **log** (text) have **neither**. For metrics, **storage is canonical-at-rest**: every metric `datapoint_type` declares **one canonical unit** in its `unit` field (a registered unit, see below), and stored values are **always** in that unit. The firehose is single-unit, so every threshold, calc, and fusion compares like with like. We never store mixed units, and we never put the unit in the `instance` dimension: `instance` discriminates co-existing values of one key on one owner (three fan speeds), not the unit those values are expressed in. A genuinely different measurement is a different `datapoint_type`, not a unit variant: units only convert **within one family**.

**The `unit` registry.** Units live in a `unit` registry grouped by **family** (dimension): temperature, data-size, bitrate, ratio, and so on. Each family declares one **canonical unit** plus zero or more **alternate units**, and each alternate carries a **`to_canonical`** and a **`from_canonical`** transform: **affine** (a factor plus offset) for the common case, or an **Expr** for the rare nonlinear one (dB). The registry is **official / org scoped** like the other registries. Example: the temperature family is canonical `celsius`; `fahrenheit` carries `to_canonical: (v - 32) * 5/9` and `from_canonical: v * 9/5 + 32`.

**Dimensionless is still a unit.** A **ratio** is not "a number with no unit", it is the `ratio`
family: canonical `ratio` (`0..1`) with `percent` as an alternate (`ratio * 100`), so `cpu.utilization`
is **stored** as `0.9` and authored or shown as `90%` through the same convert path, never stored as a
percentage. A bare **count** (people, error tallies) is a cardinal `count`, distinct from a ratio. So the
`unit` field is exactly what separates a ratio from a quantity carrying a physical unit (`celsius`,
`rpm`, `bps`): both are `metric` kind, and the **unit (its family)** is the discriminator, dimensionless
or dimensioned. (`kind` answers *metric / state / log*; `unit` answers *which dimension, if any*.)

Conversion happens only at the two edges and in expressions; the rows in between stay canonical.

**Normalize-in at the edge.** When a device reports a non-canonical unit, the component template's **alignment value-transform** (the existing "align to a canonical key, plus an optional value transform") converts native to canonical **before** the datapoint is emitted. A Fahrenheit display's template emits `celsius`. The device's native unit is a [collection](/architecture/collection/)-time fact carried by the [template](/architecture/templates/), never a storage fact.

**Convert-out on read.** Showing a non-canonical unit to an operator is a **presentation** concern: the [UI](/architecture/ui/) and [views](/architecture/views/) convert canonical to the operator's display unit (a per-user / per-locale preference), looked up from the `unit` registry, exactly as a severity level's label and color resolve client-side. Storage is untouched: one operator reads Celsius, another reads Fahrenheit, off the same rows. Because the `datapoint_type` declares the canonical unit, this conversion is automatic.

**Display precision is part of the type.** Alongside the unit (and, like the unit, only on a metric), a
`datapoint_type` carries an optional **`precision`** (significant digits to render), a presentation default the same way the canonical unit is:
a temperature shows `21.5`, a utilization `90%`, a link `1.2 Gbps`. It governs **rendering only**. The
stored `value_type` (float8) keeps full precision, `precision` never truncates a stored value, and the
[UI](/architecture/ui/) or a locale can override the default. (Dropping noise at *ingest* is a separate
collection-time rounding, not the type's display precision.)

**Expressions: `convert(value, "<unit>")`.** A stdlib function in Omniglass [expressions](/architecture/expressions/). The **source unit is inferred** from the bound datapoint's canonical unit; the **target** is a registered unit that must be in the **same family** (a compile error otherwise); the conversion comes from the `unit` registry. So `convert(value, "fahrenheit") > 100` lets an operator author a threshold in Fahrenheit while storage stays Celsius. It is data-driven and general, chosen over a per-unit method like `value.toFahrenheit()` that would need a method per unit, and is available wherever expressions run: event rule and alarm criteria, calc leaves, and list filters.

## Key scope: template, org, official

A datapoint key carries a **`scope`**, the axis that decides where its name is unique and where its trust comes from. Three layers:

| scope | identity (uniqueness) | trust | who defines it |
|---|---|---|---|
| **template** | `(template_id, name)` | local | the template author |
| **org** | `name`, unique within the deployment | local custom canonical | the org / operator |
| **official** | `name`, globally | shipped with the distro | the distro |

`official` is just `scope == official` (the prior pass's `official` boolean folds into this enum as its top value). **Conflicts are impossible** at template scope because a template-scoped key is identified by `(template_id, name)`: two templates can both declare an `input` datapoint with no collision (the Zabbix model). Trust still comes from **distribution**, not a label: an official key is trusted because it is **in the release**, the same `video.input` across every vendor, not spoofable. An org key is a deployment's own custom canonical, authoritative within that one database (per-database isolation makes it unambiguous, one database is one tenant). A template key is local to the template that minted it.

**Every datapoint is typed by a registry row, just at some scope.** The datapoint -> `datapoint_type` FK is **non-null**: template-scoped keys ARE `datapoint_type` rows (`scope=template`, with a `template_id`), not inline-only shapes, so there is no nullable type FK and no dual identity. Kind, unit, and validation live on the type row at **every** scope, so the edge compiler bakes the kind and routes to the right table (metric / state / log) the same way for all three layers. Series identity is `(owner, datapoint_type, instance, provenance)`.

**The promotion ladder is template -> org -> official.** Each step is a cheap **re-scope or re-point**, not a migration: lift a template's `input` to an org-canonical `video.input` and re-point the template's datapoint at it; later it gets blessed **official** by being shipped in the distribution (the one real way trust is earned, not a flag an operator sets). Datapoints already collected keep resolving. This is the "shift to normalized over time" path.

**Normalization is therefore optional but encouraged.** A template ships using template-scoped keys with zero registry friction; aligning a key to org or official is what buys cross-fleet comparability, dashboards, and AI. The shipped official set covers the common AV/IT signals, so most templates align by just **referencing** one. Sharing happens at the **template level** (a repo or marketplace of templates): an imported template is **linked** (tracks upstream) or **copied** (forked, diverged), and the keys it introduces land at **template** or **org** scope, not as a federated signal trust tier.

**Governance is curation, not runtime enforcement.** Omniglass is a Postgres database an operator runs, so nothing stops a self-hoster inserting an org-scoped row or editing an official one in their own database. We vouch only for what we **ship**; you vet what you import, and you own the risk. Commands sit at **template** scope the same way (functions live on the template); a canonical command type follows the same promotion ladder (see [templates](/architecture/templates/)).

## Provenance: how we know a value

Provenance is the second axis, stamped per datapoint row. The same key, with the same value, can be known three ways. Each provenance points at the immutable ground-truth record that produced it (its **lineage**), and the lineage column populated is mutually exclusive per provenance, enforced by a CHECK constraint.

| Provenance | How we know it | Lineage points at |
|---|---|---|
| **observed** | measured from a component | on-row: `source_rule` (+ version), the edge function that parsed it |
| **calculated** | derived from other datapoints | on-row: `source_rule` (+ version), the calc_rule |
| **intended** | the declared effect of a command we issued, pending reconciliation | `event_id` (the command event) |

A value of any provenance is still a metric/state/log (the kind is fixed by the key); provenance only records *how it got there*. All three land in the same datapoint tables, side by side for the same key, which is what makes divergence detection free. Declared intent is the fourth value an operator can assert, but it lives in [config](/architecture/variables/), not in the datapoint tables, and can be compared against an observed datapoint for drift.

A separate **`source`** column records *which sensor or path* produced an observed value (`codec.cec` vs `display.lan` vs `control.system`). Source is distinct from provenance: provenance is *how we know* (observed), source is *which sensor told us*. Three sensors reporting one display's power are three observed rows on one key, differing only in source. This is what makes multi-source corroboration and [fusion](#fusion) possible.

**Trace columns, orthogonal to lineage.** Each datapoint table also carries a nullable **`correlation_id`** and an optional **`caused_by_event_id`**. These are **trace** columns, not lineage: they record *what causal thread this row belongs to*, not *what immutable record produced this value*, so they sit outside the mutually-exclusive lineage CHECK and never count toward it (a row may carry both its on-row `source_rule` lineage and a `correlation_id` with no conflict). An action's command **propagates the originating `correlation_id`** onto the adaptive-poll's observed datapoint, so the `event_rule` that fires off that observed value **inherits** the same id, and the cycle-guard walk crosses the command -> device -> observed-datapoint round trip on a real carried id rather than an assumed lineage. See [alarms and actions](/architecture/alarms-actions/) for the cycle-safety mechanic.

### observed: from a component, via an edge parse

"Measured from a component," not "from a device", every device is a component, but not every component is a device. The node parses the payload at the edge and **publishes the observed datapoint to the JetStream raw ingress subject** (admission confines its owner before it reaches the trusted stream); it does not write to Postgres. The observed datapoint carries its own lineage on the row: `source_rule` + `source_rule_version` (which function and template version made it, the backtest hinge). The verbatim payload it parsed is **not** kept (no telemetry table); raw surfaces only on a `collection.failed` event or a dev raw-mode tap, or is retained for a bounded window under the opt-in `raw_sample` policy ([collection](/architecture/collection/)), which is still not a telemetry table. There is no separate execution table, a derived datapoint is itself the evidence of the function's run, exactly as an event/alarm/action row self-describes.

### calculated: derived by a calc rule

A calculated value (a 5-minute average, a system rollup, a fused consensus) is parallel to observed: both are machine-derived. The difference is the input: an edge function parses a device payload, a calc rule reads **other datapoints**. A calc consumer reads datapoints **off the trusted JetStream `datapoints` stream** and **publishes its derived datapoint back onto it directly** (a trusted server producer, no admission pass), so calculated values re-enter the data lane exactly like observed ones (and are themselves available to downstream calc and to the rule engine). Both carry `source_rule` + `source_rule_version` on the row, so they are distinguished by the **`provenance` column** (an edge function versus a calc_rule), not by a pointer. The exact inputs a calc read are reconstructable from the rule version (that is what backtest does); if an immutable input snapshot is ever needed it is a nullable `inputs jsonb` column, not a table. The rule itself lives on [calculations](/architecture/calculations/).

### intended: the declared effect of a command

When the action layer issues a command, it records the command as an **event** and writes the **intended** state it expects, in one step. The command and its event are born in the record/state lane (PG-first, CDC-out); the intended datapoint **re-enters the data lane** on the `datapoints` stream **under the command's target owner** (the target was scope-checked at dispatch; the action layer is trusted server-internal, so it publishes to the trusted stream directly, no admission pass), so the command's expected effect rides the same stream as observed and calculated values and reconciles against the observed value that the device round trip produces. The intended datapoint's lineage is that command event. The name is deliberate: **intended vs observed** is the central razor, intent-in-progress versus measured reality.

```text
1. command issued:  "power on display-5"  -> recorded as an event
2. intended write:  display-5 power = on, provenance=intended, lineage=<the command event>
                    a bet: intended, not measured
3. adaptive poll:   the command triggers a poll sooner than the normal interval
4. observed arrives:
     observed = on  -> reconciled (the bet paid off)
     observed = off -> divergence (the command did not land)
```

There is no separate "mapping" primitive. Which state a command intends lives on the command definition. **Only commands set intended state** (intended's lineage is always a command event). An external event that implies a state ("meeting started, so the room is occupied") is not intended state: it is observed reality, so it lands as an **observed** state through the ordinary edge-parse path, not the command lane.

Not every log-to-state path goes through a command. The split is measured fact vs pending intent:

| The source says | Means | Path |
|---|---|---|
| "eth0 **is** down" | a component reporting measured reality | edge parse, then **observed** state, directly |
| we sent "**power on**" | intent in progress, not yet confirmed | command, event, then **intended** state |

### declared values are config

mac, ip, serial, locked-input, anything an operator *sets* is declared intent, and declared intent is **not** a datapoint provenance. It lives in [config](/architecture/variables/): keyed to the same canonical signal as its observed side, resolved through the scope cascade, never in the datapoint tables. There is no separate property store: config is the declared side of a signal plus the cascade. Ownership resolution reads the resolved identity (a declared identity config value, or the observed identity datapoint that shares its key) to bind observed data to components, through the [identity-binding index](/architecture/collection/) (a `(datapoint_type, value) -> owner` arc) that collection maintains.

### Precedence: spec versus status lives in config

When declared intent and observed reality disagree, which one wins is a **per-config-item `reconcile` policy** ([config](/architecture/variables/#drift-and-reconcile)), not a per-key datapoint attribute:

- **observed wins** is `reconcile: observe` (or `warn`): the declaration was a hint or stale guess, reality is truth. A device reporting a different MAC than the declared one is a divergence to surface (silently under `observe`, as an alarm under `warn`); adopting the observed value as declared is a separate one-shot import action.
- **declared wins** is `reconcile: enforce`: the declaration is the spec, reality should conform. Observed input HDMI2 against a declared HDMI1 means the world is wrong, converge via the set function (self-healing, the Kubernetes spec-and-status pattern), and alarm if the set fails.

Among datapoint provenances there is no precedence contest: intended is a pending bet that observed confirms or refutes (reconciliation, see [intended](#intended-the-declared-effect-of-a-command)), and observed supersedes it on arrival. The spec-versus-status decision is config's reconcile policy, not a per-key datapoint attribute. Device-swap (where a declared MAC is briefly authoritative before the device reports it) is handled by a component's [maintenance mode](/architecture/core-entities/#operational-mode-active-maintenance-disabled), which suppresses drift.

## Ground truth versus derived

Distinguished by a property of the table, not a naming suffix.

- **Raw payload: not stored.** Datapoints are emitted at the edge, so the verbatim wire payload is **not persisted** (no `telemetry` table). Raw surfaces only on a **`collection.failed`** event when a parse or validation rejects (diagnosis, and the one backfill-after-fix case) and via a **dev raw-mode** tap; the datapoint is authoritative, its lineage is `source_rule` + version. The opt-in `raw_sample` policy ([collection](/architecture/collection/)) can retain raw for a bounded, sampled, short-lived window, off by default, still not a telemetry table.
- **Live on NATS, durable in PG.** The live datapoint is the message on the JetStream `datapoints` stream; the durable copy in the `metric_datapoint` / `state_datapoint` / `log_datapoint` tables is written by a **persistence consumer** that batch-writes off the stream as an **async sink**, idempotent on series identity. The sink never gates the rule engine: rules read datapoints from NATS, and a slow or paused persistence consumer holds up only the durable record, not the live signal. Datapoints are the firehose, so they reach Postgres through the sink and **do not go through CDC**, unlike the record/state lane (events, alarms, actions), which is born in a PG transaction and fanned out by CDC.
- **Ground truth, logs** (immutable, append-only, the actor's own record): **`log_datapoint`** (a component's words, a datapoint kind), **`audit_log`** (an operator), **`session_log`** (connection lifecycle, node-reported), **`internal_log`** (platform self-narration), and the **`collection_log`** / **`node_log`** companions. Each named for what it is. There is no separate rule-execution table: a derived row *is* the evidence of its rule's run, carrying `source_rule` + `source_rule_version` on the row itself.
- **Derived** (produced by rules, reconstructable in principle from ground truth): **`metric_datapoint`**, **`state_datapoint`**, **event**, **alarm**, **action**.

A datapoint's lineage is `source_rule` + version (the function that made it). The companions extend it: `collection_log` is the cheap per-run execution record (every run, including failures), `node_log` the node's operational narration. A failed parse rides a `collection.failed` event carrying the raw; there is no telemetry table in the chain. See the architecture overview on the spine.

## The DAG invariant

The pipeline must stay acyclic.

> A rule may **read** observed and calculated values as truth. It may **compare** an intended value, or config's declared value, against observed (drift). It may **not** treat an intended value *as truth* to infer a new fact.

This is what makes drift safe: a drift rule reads the *pair* (intended, observed) and emits when they disagree; the intended value is tested, not trusted. The one forward edge command-to-intended-state is terminal (nothing reads back from an intended value to produce more state). Event rules reading only observed/calculated keeps the graph acyclic with no runtime cycle guard required.

The command -> device -> observed-datapoint round trip is the one path where the acyclic structure cannot be read off the static graph (a command can provoke an observed value that fires the rule that issued the command). The propagated `correlation_id` closes that gap: because the command stamps its id onto the observed datapoint, the run that fires off that observed value carries the same thread, and the cycle-guard walk follows a **real carried id** across the round trip rather than inferring lineage. The DAG invariant is therefore enforced, not merely assumed, on the only edge that needs runtime help.

## disagree and divergence

Drift is a condition operator, **`disagree(A, B)`**, usable inside event rule conditions, comparing two provenances (or two sources) of one key:

- `disagree(intended, observed)`: the command did not land (reconciliation)
- `disagree(declared, observed)`: the world drifted from intent (config drift, device swap); the declared side is read from [config](/architecture/variables/)
- `disagree(observed, observed)` across `source`: sensors conflict (a failing sensor)

> Any two provenances of the same key that disagree = an anomaly. One detector.

Command reconciliation, configuration drift, sensor conflict, and hardware-swap detection are not separate features; they are one comparison applied to a key that can hold more than one provenance.

## Fusion

When multiple sources report one signal, they land as **perspectives**: source-tagged observed rows differing only by `source`, **all preserved** (seeing multiple perspectives on one value is itself instructive). A reduce-on-read **policy** produces the effective value. Fusion splits by whether the inputs describe the same key:

- **same-key, many sources** keeps every perspective and reduces on read. The key's **`fusion_policy`** on `datapoint_type` is a **default/hint, not a mandate**: the right reduce often is not knowable a priori at the `datapoint_type` level (for `display-5.power` from codec CEC, display LAN, and the control system, you cannot know how to fuse the value before considering the actual sources). So a policy may **default from the type**, but can be source-weighted, per-instance, or **left open**: keep all perspectives and decide at read time, by an operator, or by AI. When a policy reduces (`mode`: priority / weighted / majority / worst / average / latest, plus tie-break and optional per-source weights), the reduced value is what `current_value` and event_rule evaluation read; the source-tagged perspectives stay, so "which source is wrong" remains queryable. `event_rule` evaluation reduces over the **latest-per-source perspective set** for the owner and key, held from the live `datapoints` stream (a bounded, in-memory set), never a firehose scan of the durable tables. This improves *confidence in a reading*. A `source` registry carries default trust weights, so the simplest case needs no config. Materialize a fused series only if a profile earns it.
- **cross-key / system-level** is a **`calc_rule`** (the only fusion that authors a rule): `room.in_use` derived from display power + codec call-state + occupancy. This *derives a higher-order fact*, a new key, not a same-key consensus. See [calculations](/architecture/calculations/).

Conflict detection (`disagree(observed, observed)` across sources) is the complementary operation: even when an effective value is usable, a perspective disagreeing beyond tolerance is itself a signal.

## Reads: current value is a view

Current value (latest per owner / key / **instance** / **provenance**, reduced across the source perspectives per the effective `fusion_policy`) is a **view** over the persisted tables, correct and zero-maintenance. It is keyed per-provenance because "current observed power" and "current intended power" are different values for the same key, and the divergence model depends on seeing both. A materialized `current_value` table is a measured optimization, earned when a read profile proves the view too slow: the driver is **operator and fleet-dashboard reads**, not the rule engine, since the rule engine evaluates against datapoints live off the JetStream `datapoints` stream and never reads the view. The same view-by-default discipline as storage applies. Ownership resolution reads resolved identity config (the declared value, else the observed [identity datapoint](/architecture/collection/)) by targeted indexed lookup, not a full scan, so it does not by itself justify the materialized table.

## The datapoint tables

The three kinds are three physical tables only because they index and retain differently; the [physical layout, partitioning, and the lineage CHECK](/architecture/storage/) live on storage.

| Table | Key columns | Notes |
|---|---|---|
| `metric_datapoint` | id, ts, **owner_kind, component_id/system_id/location_id/node_id**, key, **instance**, **value float8**, provenance, source, **source_rule, source_rule_version, event_id**, **correlation_id?, caused_by_event_id?** | the firehose; BRIN on ts; numeric aggregation. `instance` (`''` default) discriminates many values of one canonical key on one owner. `correlation_id` / `caused_by_event_id` are nullable trace cols, outside the lineage CHECK |
| `state_datapoint` | id, ts, owner arc, key, instance, **value text/jsonb**, provenance, source, + same lineage and trace cols | sparse, transition-only; time-in-state and dwell. [Config](/architecture/variables/) is keyed to one as its observed side |
| `log_datapoint` | id, ts, owner arc, key, instance, **value text/jsonb (the line)**, level, provenance, source, + same lineage and trace cols | GIN / tsvector full-text; also the holding pen for un-normalized occurrences |

Common datapoint columns (all three kind-tables): `ts`, the **owner arc** (`owner_kind` plus `component_id` / `system_id` / `location_id` / `node_id`), `key, provenance, source`, the on-row lineage `source_rule, source_rule_version, event_id`, and the nullable trace columns `correlation_id, caused_by_event_id` (outside the lineage CHECK); only the value column differs (float8 / text-jsonb / line). A `datapoint` view UNIONs the common columns for "all datapoints for owner X".

The key registry that types these tables is `datapoint_type` (one registry across all three kinds), detailed at [the datapoint_type registry](#the-datapoint_type-registry):

| Table | Key columns | Notes |
|---|---|---|
| `datapoint_type` | name, **scope** (template/org/official), **template_id?**, kind (metric/state/log), value_type, unit, **precision**, **fusion_policy**, validation (jsonb) | the one key registry across all datapoint kinds; `scope` decides where the name is unique (`(template_id, name)` at template scope, `name` at org/official); referenced by templates, which also mint their own template-scoped rows. `unit` is the **canonical** unit, a row in the `unit` registry below; `precision` is a display hint (significant digits), not a storage truncation. Both apply to **metrics**; a **state** or **log** has neither |
| `unit` | name, **family** (temperature/data-size/bitrate/...), **canonical** (bool), **to_canonical**, **from_canonical** (affine factor+offset, or Expr), **scope** (official/org) | the unit registry: one canonical unit per family plus alternates each carrying its conversion transforms; the `datapoint_type.unit` canonical unit references it, and `convert(value, "<unit>")` resolves same-family targets through it |

## The pipeline, end to end

```d2
direction: down
classes: {
  node: { style.border-radius: 8 }
  key: { style: { border-radius: 8; bold: true } }
  group: { style.border-radius: 8 }
}
edge: "Edge (node)" {
  class: group
  task: "task\npoll · listen\nstateless / stateful" { class: node }
  fn: "function\nextract → key → normalize" { class: node }
  task -> fn
}
raw: "raw ingress\nnode · webhook (untrusted)" { class: node }
admit: "admission consumer\nowner-confine per class\n(system mode)" { class: node }
ds: "JetStream\ntrusted datapoints stream" { class: node; shape: queue }
failed: "collection.failed\n(carries raw)" { class: node }
calc: "calc_rule consumer\ncross-key · system-level" { class: node }
erule: "event_rule consumer\nfire_criteria (+ optional clear_criteria)" { class: node }
persist: "persistence consumer\nbatch sink (async)" { class: node }
tables: "metric · state · log\ndatapoint tables" { class: node; shape: cylinder }
sched: "schedule + timer\n(leader-elected clock)" { class: node }
pg: "event · alarm\n(PG)" { class: node; shape: cylinder }
alarm: "alarm\none incident · new row per open\n(event_rule, owner)" { class: node }
cdc: "JetStream\nrecord/state lane" { class: node; shape: queue }
actions: "action_rule consumer\nnotify · command\nremediate-verify-escalate" { class: node }
itsm: "ITSM (action target)" { class: node }
operator: operator { class: node }
config: "config\ndeclared (spec)" { class: node }
audit: audit_log { class: key }
divergence: divergence { class: node; shape: hexagon }
edge.fn -> raw: "observed · lineage on row\n(source_rule)"
edge.fn -> failed: "parse / validation fail" { style.stroke-dash: 4 }
raw -> admit
admit -> ds: "confined"
ds -> calc
calc -> ds: "calculated · trusted producer\n(direct, no admission)"
ds -> erule
ds -> persist
persist -> tables: "durable copy"
sched -> erule: "origin=scheduled"
erule -> pg: "PG-first: event + alarm in one tx"
pg -> alarm: "alarm transition"
pg -> cdc: "CDC (logical decoding)\nleader-elected publisher" { style.stroke-width: 3 }
cdc -> actions
actions -> ds: "command's effect · provenance=intended\n(trusted, direct)" { style.stroke-dash: 4 }
actions -> itsm: "ITSM: open->ticket · update->comment · resolve->close" { style.stroke-dash: 4 }
actions -> edge.task: "command + adaptive poll" { style.stroke-dash: 4 }
operator -> config: "declares (PG-first)"
config -- tables: "links · drift" { style.stroke-dash: 4 }
operator -> audit: "audit" { style.stroke-dash: 4 }
cdc -- divergence: "disagree(A,B): drift / conflict" { style.stroke-dash: 4 }
```

Two lanes, one bus. The **data lane** is the JetStream **trusted** `datapoints` stream. Untrusted publishers (the edge node, an external webhook) land on a **raw ingress** subject; an **admission consumer** owner-confines each datapoint against the publisher's placement (or the webhook interface's declared owner) and re-publishes only confined points to the trusted stream, so a forged owner is dropped before the live `event_rule` can act on it ([identity and access](/architecture/identity-access/)). **Trusted server producers** (calc output, a command's intended write) publish to the trusted stream directly, no admission pass. The `event_rule` consumer evaluates against the trusted stream live, and a **persistence consumer** batch-writes the three datapoint tables (`metric_datapoint`, `state_datapoint`, `log_datapoint`) as an async sink (datapoints never go through CDC). The **record/state lane** is PG-first: an `event_rule` fire writes the event and alarm transition to PG in one transaction, and a leader-elected **CDC publisher** (logical decoding of the WAL) fans those committed changes onto JetStream, where `action_rule` consumers react. A command's intended datapoint re-enters the data lane (the device round trip). The teal node is `audit_log`, the ground-truth record of operator writes (including config changes); observed and calculated carry `source_rule` on the row, intended points at the command `event` (via `event_id`). The raw payload is not stored: a parse or validation failure rides a `collection.failed` event. [config](/architecture/variables/) holds declared intent (PG-first), keyed to a state datapoint as its observed side.

Related: [events](/architecture/events/) (the event family and `event_type`), [calculations](/architecture/calculations/) (calc rules and the rule families), [config and credentials](/architecture/variables/) (declared config, drift, reconcile), [collection](/architecture/collection/) (how telemetry arrives), [alarms and actions](/architecture/alarms-actions/) (alarm lifecycle, actions), and [the glossary](/architecture/glossary/) (every term defined once).
