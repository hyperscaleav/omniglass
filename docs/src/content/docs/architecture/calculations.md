---
title: Calculations
description: "The rule families that run server-side over typed samples, and calc_rule in detail: cross-key and system-level derivation."
sidebar:
  badge:
    text: Partial
    variant: note
---

:::note[The event rule is built; the calc rule's registry is built, its evaluation is not]
Of the three rule families below, the **`event_rule`** is built (#819,
[ADR-0144](/architecture/decisions/#adr-0144-alarms-are-raised-by-event-rules-at-a-severity-level-from-a-ranked-registry)):
its own table with typed columns, evaluated in the ingest transaction rather than on a JetStream
consumer, and documented on [alarms and actions](/architecture/alarms-actions/#the-event_rule). It
writes its typed fire and clear events at each edge of its condition, its alarm optional
([ADR-0145](/architecture/decisions/#adr-0145-a-rule-records-its-conditions-as-typed-events-and-its-alarm-is-optional)).
The **`calc_rule` registry** is built (#995, [ADR-0147](/architecture/decisions/#adr-0147-a-calc-rule-is-a-typed-sibling-registry-checked-acyclic-at-authoring)):
rules are authored, type-checked and kept acyclic, below. Running them (the pending set, the tick, the
reducers and the calculated write) is the rest of epic #525, and the `action_rule` is still design.
:::

## The calc_rule registry

A **calc rule** derives one **output key** on each of its owners from named **inputs**. It is a
sibling table of `event_rule` (ADR-0144 ruling 1), with typed columns rather than one spec:

- **`owner_kind`**: `component` (each component its `selector` picks, the event rule's selector
  shape: component types and components, both empty for every component) or `system` (every system;
  a system rule's selector is empty until system-level evaluation lands). Fixed at create.
- **`inputs`**: one to sixteen `{as, key}` pairs. `key` is a registered property or metric, read once;
  `as` is the name the reduce knows it by, an identifier that is no keyword and no builtin's name.
- **`reduce`**: a **named reducer** `{reducer}` or an **expression** `{expr}`, exactly one.
- **`output_key`**: a registered property or metric, held by its id in its lane's registry (the lane
  recorded in `output_lane`), so a key a rule writes cannot be deleted.
- `name` (unique, renameable), `label`, `description`, `enabled`, `official` (read-only), `version`
  (bumps on every update; a write that changes nothing writes nothing).

Rules read like a registry, gated `rule:read` on any grant, and are written with `rule:*` on an
all-scope grant, as event rules are; every write is audited on the rule's uuid and returns it. A rule
is addressed by its name or its uuid, and `POST /calc-rules/{name}:rename` moves the name
([ADR-0076](/architecture/decisions/#adr-0076-a-renameable-human-typed-identifier-stays-in-the-url-and-the-write-returns-the-uuid)):
nothing inside the platform holds it, so a rename bumps no version. The routes are on
[the API](/architecture/api/).

### What a reduce may say

The reduce is checked against the **calc site**, an [expression](/architecture/expressions/) site of
its own (its bindings, reducers, builtins and budget are in the generated sites table there):

- A **named reducer** folds every value of the rule's **one** input: on a component, every instance of
  the input key on that component; on a system, every instance on every role-bound member. It yields
  one value, so the output is aggregate (`instance = ''`). `average`, `min`, `max` and `sum` fold a
  metric into a number; `count` counts the values present, of any type; `majority` folds a property into its own type; `any` and `all` fold a
  bool property into a bool.
- An **expression** reads the inputs by name. For a component owner each name is the component's
  current value of its key on the aggregate instance, typed by the key (a metric a number, a property
  text, a bool property a bool); for a system owner each name is the list of those values, one per
  role-bound member. A key reported per instance is folded by a named reducer.
- Either way the result must be the **output key's type**; an expression yielding anything else is a
  type error at line 1, and a fault inside one is a 422 at its line and column.

### The authoring checks

Every write is refused, with a 422 naming the field at fault, when:

- **it would close a cycle.** The graph runs from each enabled rule's input keys to its output key, and
  a rule whose output can reach one of its own inputs is refused with the chain of keys
  (`temp-twice -> avg-temp -> temp-twice`) and the rules along it. The graph is read under a
  transaction lock, so two writes cannot each close half a loop; a disabled rule is outside the graph
  until it is enabled, and enabling it is checked. This is a topology check beside
  [ADR-0069](/architecture/decisions/#adr-0069-cycle-safety-is-provenance-based-not-topology-based)'s
  provenance rule, not in place of it: a loop through an event rule and an alarm is not in this graph.
- **it reads or writes `health`.** An alarm moves health, so a rule reading it could feed back through an
  event rule, and health has its own producer, the rollup.
- **its output is a key another enabled rule writes.** One enabled rule writes a key: two would mix
  their values on an owner by newest timestamp. The refusal names the other rule, and enabling a
  disabled rule is checked the same way.
- **its output is a key a collection writes**, for the same reason. The check reads declarations, never
  sample history: the native probes' keys (`internal/collection/probekeys`, the one declaration the
  probes emit from) and any stored driver whose spec declares the key among its emits. What it cannot
  catch is a driver created or edited to emit the key after the rule exists.

The graph and the one-writer check are key-level: they ignore owner kinds, so a component rule and a
system rule writing or reading the same key are judged as one graph. That can refuse a rule that would
never meet the other on an owner, never admit a loop, so it is deliberate and safe.

A patch is judged on what it changes: unchanged inputs and reduce are not recompiled (so a rule whose
input key was since deleted stays editable), and an unchanged output is not re-checked for collectors.
Enabling a rule recompiles it. A stored reduce that no longer compiles (an input key deleted or
retyped, a row written outside the gateway) is not silent: every read carries `calc_error` (the field,
the line and column when the expression is what failed, and the message), compiled once per rule
version and key typing, and such a rule cannot be enabled until it compiles. Deleting a property or
metric type a rule writes is a 409 naming the rule.

:::design[Target design: running calc rules, tracked in #525]

Parsing a raw payload into samples is the **edge function** ([collection](/architecture/collection/)), not a server-side rule. The rules that run server-side over the typed samples are two derivation families plus a subscription; this page owns the calc family.

The rule families run as **JetStream consumers on the data lane**: confined samples arrive on the NATS **trusted** `samples` stream, and rules never wait on Postgres. A calc consumer **publishes** its derived samples back onto the trusted stream as a trusted server producer (no admission pass; the calc owner comes from the validated `calc_rule` scope); an event consumer, on fire, writes to Postgres on the record lane, CDC-published. The lane model lives on [messaging](/architecture/messaging/).

## Rules: calc, event, action

- **calc_rule**: samples to sample (calculated); this page.
- **event_rule**: sample change to event. Lives on [events](/architecture/events/) and [alarms and actions](/architecture/alarms-actions/): a required `fire_criteria` and an optional `clear_criteria`, the fire/clear pair opening and resolving an alarm. Built (#819) as its own table, raising and clearing alarms directly, and (#938) writing its typed fire and clear `event` rows at each edge, carrying the alarm; the alarm is optional.
- **action_rule**: a subscription wiring events and alarms to actions. Lives on [alarms and actions](/architecture/alarms-actions/).

There is no `alarm_rule` and no `condition_rule`: an alarm is an event rule whose events are paired (open, close). Ownership for a templated function is stamped at the edge; shared-endpoint ingress is owner-bound server-side. A **`discovery_rule`** (observed data creates entities) rounds out the family.

## calc_rule: cross-key and system-level derivation

A **calc_rule** owns inputs, a reduce (worst / majority / average / Expr), an output key, and a scope; downstream consumers see its output like any other sample. It is for **cross-key** and **system-level** derivation: a 5-minute average, a system rollup, `room-in-use` derived from display power + codec call-state + occupancy. Same-key multi-source reconcile is the key's `fusion_policy`, not a calc (see [Fusion](/architecture/properties/#fusion)).

The calculated value is parallel to observed, distinguished by the **`provenance` column**, both carrying `source_rule` + `source_rule_version` on the row ([calculated](/architecture/properties/#calculated-derived-by-a-calc-rule)).

Calc folds **every** instance of an input name into the reduce: a rule reading `fan-speed` gets one candidate per fan. Calc **outputs** stay aggregate (`instance = ''`); per-instance outputs (a group-by) are a separate capability, and output owners default to the singleton ([the instance dimension](/architecture/properties/#the-instance-dimension-many-values-of-one-name-on-one-owner)).

Cross-key / system-level fusion is the only [fusion](/architecture/properties/#fusion) that authors a rule, deriving a higher-order fact (a new key) rather than reconciling one key across sources.

## The DAG invariant

Calc rules read observed and calculated values as truth, never intended, which keeps the pipeline acyclic; stated in full on [samples](/architecture/properties/#the-dag-invariant).

## Storage

The three rule families share one config shape; the physical layout lives on [storage](/architecture/storage/).

| Table | Key columns | Notes |
|---|---|---|
| `calc_rule` / `event_rule` / `action_rule` | **(id, version)**, scope, spec (jsonb: Expr + params) | config, named for function; versioned so a backtest can pin the rule version. The built `event_rule` and `calc_rule` diverge where their rulings say: typed columns rather than one spec, and a single `version` integer that bumps on update, so the backtest key is a later additive widening ([alarms and actions](/architecture/alarms-actions/#storage)). `calc_rule` = cross-key/system-level derivation; `event_rule` = fire_criteria + optional clear_criteria ([events](/architecture/events/), [alarms and actions](/architecture/alarms-actions/)); `action_rule` = a subscription (an Expr predicate over events). Parsing is the edge function, not a rule; `discovery_rule` rounds out the family |
:::
