---
title: Calculations
description: "The rule families that run server-side over typed samples, and calc_rule in detail: cross-key and system-level derivation."
sidebar:
  badge:
    text: Design
    variant: caution
---

:::design[Target design: the calc rule family, tracked in #525]

Parsing a raw payload into samples is the **edge function** ([collection](/architecture/collection/)), not a server-side rule. The rules that run server-side over the typed samples are two derivation families plus a subscription; this page owns the calc family.

The rule families run as **JetStream consumers on the data lane**: confined samples arrive on the NATS **trusted** `samples` stream, and rules never wait on Postgres. A calc consumer **publishes** its derived samples back onto the trusted stream as a trusted server producer (no admission pass; the calc owner comes from the validated `calc_rule` scope); an event consumer, on fire, writes to Postgres on the record lane, CDC-published. The lane model lives on [messaging](/architecture/messaging/).

## Rules: calc, event, action

- **calc_rule**: samples to sample (calculated); this page.
- **event_rule**: sample change to event. Lives on [events](/architecture/events/) and [alarms and actions](/architecture/alarms-actions/): a required `fire_criteria` and an optional `clear_criteria`, the fire/clear pair opening and resolving an alarm.
- **action_rule**: a subscription wiring events and alarms to actions. Lives on [alarms and actions](/architecture/alarms-actions/).

There is no `alarm_rule` and no `condition_rule`: an alarm is an event rule whose events are paired (open, close). Ownership for a templated function is stamped at the edge; shared-interface ingress is owner-bound server-side. A **`discovery_rule`** (observed data creates entities) rounds out the family.

## calc_rule: cross-key and system-level derivation

A **calc_rule** owns inputs, a reduce (worst / majority / average / Expr), an output key, and a scope; downstream consumers see its output like any other sample. It is for **cross-key** and **system-level** derivation: a 5-minute average, a system rollup, `room.in_use` derived from display power + codec call-state + occupancy. Same-key multi-source reconcile is the key's `fusion_policy`, not a calc (see [Fusion](/architecture/properties/#fusion)).

The calculated value is parallel to observed, distinguished by the **`provenance` column**, both carrying `source_rule` + `source_rule_version` on the row ([calculated](/architecture/properties/#calculated-derived-by-a-calc-rule)).

Calc folds **every** instance of an input key into the reduce: a rule reading `fan.speed` gets one candidate per fan. Calc **outputs** stay aggregate (`instance = ''`); per-instance outputs (a group-by) are a separate capability, and output owners default to the singleton ([the instance dimension](/architecture/properties/#the-instance-dimension-many-values-of-one-key-on-one-owner)).

Cross-key / system-level fusion is the only [fusion](/architecture/properties/#fusion) that authors a rule, deriving a higher-order fact (a new key) rather than reconciling one key across sources.

## The DAG invariant

Calc rules read observed and calculated values as truth, never intended, which keeps the pipeline acyclic; stated in full on [samples](/architecture/properties/#the-dag-invariant).

## Storage

The three rule families share one config shape; the physical layout lives on [storage](/architecture/storage/).

| Table | Key columns | Notes |
|---|---|---|
| `calc_rule` / `event_rule` / `action_rule` | **(id, version)**, scope, spec (jsonb: Expr + params) | config, named for function; versioned so a backtest can pin the rule version. `calc_rule` = cross-key/system-level derivation; `event_rule` = fire_criteria + optional clear_criteria ([events](/architecture/events/), [alarms and actions](/architecture/alarms-actions/)); `action_rule` = a subscription (an Expr predicate over events). Parsing is the edge function, not a rule; `discovery_rule` rounds out the family |
:::
