---
title: Glossary
description: "The authoritative glossary: every official term in the architecture, defined once."
sidebar:
  badge:
    text: Spec
    variant: caution
---

This is the **authoritative glossary**: every official term in the architecture, defined once. The other pages introduce these terms in **bold** as the story reaches them; this is where you look any of them up.

| Term | Definition |
|---|---|
| **node** | Edge process (`--mode node`); pulls and runs tasks and commands over interfaces; carries placement, heartbeat, bound credential. |
| **function** | A trigger plus a DAG of steps, declared in a component template; the unit of edge collection. Triggered by a schedule (poll), incoming data (listen), or a command. See [collection](/architecture/collection/). |
| **flow** | A multi-step **action** (branching, parallel steps, waits); an escalation is the canonical case. See [alarms and actions](/architecture/alarms-actions/). |
| **task** | A node's unit of collection: **poll** (we ask) or **listen** (we wait), over a stateless or stateful (session) interface. Content-addressed. |
| **interface** | A connection to a component, declared once per protocol; transport stateless or stateful (to a session). |
| **interface_type** | Protocol-and-style registry (ssh, https, snmp, mqtt, webhook...); built-flag + param schema. |
| **session** | A stateful interface's live held-open connection; a current-state view over `session_log`. |
| **collection.failed** | The event emitted when a parse or validation rejects; carries the raw payload for diagnosis and backfill-after-fix. There is no stored telemetry table; raw is not otherwise persisted (a dev raw-mode taps it live). |
| **datapoint** | An observation: a key's value on one owning entity at one time, with provenance + source + on-row lineage. Kinds: metric, state, log. |
| **metric_datapoint** | Numeric (float8) datapoint. Continuous, aggregatable. The firehose. |
| **state_datapoint** | Categorical/text/object datapoint. Discrete, dwell-measurable. [Config](/architecture/variables/) is keyed to one as its observed side. |
| **log_datapoint** | A component's own log lines; value = the line. A stream; also the holding pen for un-normalized occurrences. |
| **kind** | What a key is: metric, state, or log. Fixed per key at definition. |
| **key** | The identity of what is measured or asserted; registered in `datapoint_type`. |
| **canonical signal** | A registered, owner-agnostic measurement name (`power.state`, not `room.power`); one comparable signal across every vendor. |
| **owner / owner_kind** | A datapoint/event/alarm's subject, the exclusive-arc: `owner_kind` + the matching typed FK (`component_id`/`system_id`/`location_id`/`node_id`), or the singleton `global` (no FK), + CHECK. |
| **datapoint_type** | Registry for datapoint keys: name, `scope`, kind, value_type, unit, fusion_policy, validation. `scope` (template / org / official) decides where the name is unique: `(template_id, name)` at template scope, `name` at org/official. Every datapoint is typed by one (the FK is non-null). Promotes template -> org -> official by re-scope/re-point. |
| **scope** | A key's uniqueness-and-trust axis on `datapoint_type`: **template** (`(template_id, name)`, the template author's, local), **org** (`name` within the deployment, the operator's custom canonical), **official** (`name` globally, shipped with the distro). `official` = the top scope (folds in the prior `official` boolean). |
| **template-scoped / org-scoped** | A key minted at `scope=template` (local to one template, `(template_id, name)`) or `scope=org` (a deployment's own canonical, unique by `name`). The promotion ladder lifts template -> org -> official. |
| **event_type** | Registry for event keys: name, display_name, payload_schema, `scope`. Supports the same template / org / official `scope` as `datapoint_type` (a template can define a template-local event). |
| **provenance** | How we know a value: observed, calculated, intended. Per row. Declared intent is [config](/architecture/variables/). |
| **observed** | Measured from a component. On-row lineage: `source_rule` (+ version), the edge function. |
| **calculated** | Derived from other datapoints by a calc_rule. On-row lineage: `source_rule` (+ version), the calc_rule. Distinguished from observed by the `provenance` column. |
| **intended** | A command's declared effect, pending reconciliation. Lineage: the command `event_id`. Only commands set it. |
| **source** | Which sensor/path produced an observed value; distinct from provenance; enables multi-source rows + fusion. A `source` registry carries default weights. |
| **perspectives** | The source-tagged observed rows for one signal: multiple sources reporting one value, all preserved; a reduce-on-read policy produces the effective value, while every perspective stays queryable. |
| **fusion_policy** | Per-key reduce-on-read **default/hint** for multi-source observations (mode + tie-break + source weights), not a mandate: a policy may default from the type but can be source-weighted, per-instance, or deferred to read time. Applied on read. |
| **fusion** | Reading one effective value from multiple **perspectives** on a signal: same-key multi-source reduces by a policy (read-time, defaulting from the key's fusion_policy); cross-key/system-level = a calc_rule. Perspectives are always preserved. |
| **config** | The declared side of a canonical signal: an operator-set value keyed to a `datapoint_type`, reconciled against the observed datapoint via the template's get/set functions and a per-item `reconcile` policy. See [config and credentials](/architecture/variables/). |
| **credential** | An access secret with a structured shape, a pluggable `SecretProvider` (inline or external), and a lifecycle (refresh / rotation / expiry); read is `secret:read`-gated and every decrypt audited. Template-driven. |
| **variable** | A free interpolated value (a macro): `$var:<name>`, resolved global→template→instance down the cascade; org-keyed, not signal-bound, no observed side. |
| **drift** | The gap between config's declared value and its observed datapoint, on one signal key. |
| **reconcile** | Per-[config](/architecture/variables/) item policy for drift, one of three modes: `audit` (record drift, no alarm), `warn` (alarm at warning severity), `enforce` (call the set function to converge, alarm on set failure). Adopting the observed value as declared is a separate one-shot import action, not a mode. |
| **cascade** | Resolves the effective config / variable value (declared or template default): global, component_template, system_template, then the location / system / component trees (weight-free, pure depth); most-specific (deepest) wins. Type is not a layer (it resolves via a group filter); groups are placed by weight on the same specificity scale. |
| **edge parse** | A function parses a raw payload into datapoints on the node, the edge half of [collection](/architecture/collection/). There is no server-side transform rule. |
| **calc_rule** | datapoint(s) to datapoint (calculated): cross-key / system-level derivation. (Same-key multi-source reconcile is the key's fusion_policy.) |
| **event_rule** | datapoint change to event: fire_criteria + optional clear_criteria (clear makes events alarm-paired); an optional `health` impact lets its alarm move the owner's health. No separate alarm or condition rule. |
| **action_rule** | A subscription (Expr over events; alarms via edge events) wiring occurrences to actions. |
| **discovery_rule** | *(deferred)* observed data creates components/systems/locations + their identity config; carries the `official` boolean. |
| **event** | A discrete semantic occurrence the action layer reacts to. Keyed, point-in-time, owned via the arc. Not a datapoint. |
| **origin** | How an event arose: caught, caused, derived, scheduled. |
| **alarm** | One open-to-close incident: a stateful row driven by an event_rule's paired events; new row per open; keyed (event_rule, owner); optionally health-impacting while open. Not event-sourced. The ITSM anchor. |
| **severity** | An alarm's alert importance, set to a **severity level** by id; distinct from health (a different axis). Rules and action_rule predicates compare by level (resolved via the level's order). |
| **severity level** | A registry row: `id`, `label`, `color`, and an integer `order` (for comparison only). Official defaults ship spaced; an operator can add, relabel, or recolor. Carries the `official` boolean. |
| **action** | An ordered sequence of steps (notify, command in v1; wait/branch deferred). Single-step `notify` / `command` actions ship v1; multi-step flows (including remediate-verify-escalate) are deferred. |
| **command** | A `run`-action declaration in a component_template version (not a table); an instance is an `action` with `kind=command`. |
| **disagree(A,B)** | A condition operator comparing two provenances or sources of one key. Drift, config drift, conflict. Keeps the DAG. |
| **divergence** | Any two provenances or sources of one key that disagree. The universal anomaly signal. |
| **lineage (on-row)** | A derived row carries its own lineage; no execution table. The rule version is the backtest hinge. |
| **correlation id** | A read-side trace id threading one causal chain end to end: the originating event through every downstream event and action it caused (event -> alarm -> flow/action -> command). Built on the causation lineage; `alarm_id` links one alarm's open/clear events, the correlation id links the whole chain. DX/observability sugar, not a datapoint kind or a stored span subsystem. |
| **schedule** | Config: a recurring definition (cron/rrule + IANA tz + what it triggers). |
| **timer** | The clock worker's pending-fire working set (schedule-tick / for-sustain / runbook-wait / watchdog); drained SKIP-LOCKED; not history. |
| **component** | A deployed instance (device/app/service); owns datapoints; a variable-depth tree; pins a component_template_version; classified by component_type. |
| **component_type** | Classification + field schema + type-level defaults. Carries the `official` boolean. |
| **component_template / _version** | The device shape (collection, commands, datapoint_types, defaults, alarms); the **immutable version** instances pin. |
| **system** | A composition of components/subsystems (the service tree); pins a system_template_version; located at a location; classified by system_type. |
| **system_template / _version** | The system shape; the immutable version is the snapshot instances pin. Carries a frozen BOM of member roles + health_role. |
| **location** | A place tree; classified by location_type; no template. |
| **global** | The singleton estate root: the top owner above every location where estate-wide health and KPIs roll up, and the top of the cascade. One per deployment, no FK. |
| **KPI** | A shipped derived datapoint (a calc / SLI) owned at system / location / global: availability (health over time) and the utilization family (occupancy, time, booking, ghost). An official default set with an escape hatch. |
| **SLI** | Service Level Indicator: a `time_in_state` calc datapoint over a window (e.g. `system.availability`). See [health](/architecture/health/). |
| **SLO** | Service Level Objective: the target config value the SLI must hold (availability >= 99.9%). See [health](/architecture/health/). |
| **SLA** | Service Level Agreement: meeting the SLO, an `event_rule` firing on breach; compliance over the window is itself an SLI. See [health](/architecture/health/). |
| **tag** | Operator label (registry + bindings); union + override. |
| **group** | A named set (component/system/location/user), static or dynamic, weighted; a cascade overlay + access scope. |
| **health** | The first-class operational state of every entity (up/degraded/down/unknown), carried as a *calculated* state_datapoint: `worst` over its open health-impacting alarms, rolled up the system tree role-aware. A model, not just a rule. See [health](/architecture/health/). |
| **health impact** | An optional `down`/`degraded` tag on an `event_rule`: while the alarm it opens is open, it moves its owner's health by that much. What makes health alarm-sourced. |
| **health_role** | A member's role in its system's health rollup (required / redundant / informational), declared on the system_template_member; the knob for the built-in role-aware rollup. |
| **view** | A named query returning a uniform `{columns, rows}`; the read side, executed through the scoped gateway. |
| **Storage Gateway** | The single door to the database; every read and write goes through it, and scope is injected here. |
| **audit_log** | Who-did-what ground truth; one row per operator write, same-tx; the lineage target for operator writes, including config changes. |
| **session_log** | Connection-lifecycle transitions (node-reported, diagnostic). |
| **internal_log** | Platform self-narration (startup, reconcile, migration, node-reg, config-sync). |
| **ground truth** | Immutable append-only records: log_datapoint, audit_log, session_log, internal_log. |
| **principal / role / grant** | IAM subject; an RBAC capability set crossed with a scope. |
| **secret:read** | The IAM permission to read a credential in plaintext; gated per role, and every decrypt is audited. |
| **file / blob** | Searchable metadata over content-addressed bytes (pgblobs/S3/disk); dedup. |
