---
title: Calculations
description: "The rule families that run server-side over typed datapoints, and calc_rule in detail: cross-key and system-level derivation."
sidebar:
  badge:
    text: Spec
    variant: caution
---

Parsing a raw payload into datapoints is the **edge function** ([collection](/architecture/collection/)), not a server-side rule: a function extracts, keys, and normalizes on the node and emits resolved datapoints. The rules that run server-side over the typed datapoints are two derivation families plus a subscription, and this page is the home of the calc family.

## Rules: calc, event, action

- **calc_rule**: datapoints to datapoint (calculated). The subject of this page (below).
- **event_rule**: datapoint change to event. Lives on [events](/architecture/events/) and [alarms and actions](/architecture/alarms-actions/): it carries a required `fire_criteria` and an optional `clear_criteria`, with the fire/clear pair opening and resolving an alarm.
- **action_rule**: a subscription wiring events and alarms to actions. Lives on [alarms and actions](/architecture/alarms-actions/).

An alarm is not produced by a different rule; it is an event rule whose events are paired (open, close), so there is no `alarm_rule` and no `condition_rule`. Ownership for a templated function is stamped at the edge (the component is known); shared-interface ingress is owner-bound server-side. A deferred **`discovery_rule`** (observed data creates entities) rounds out the family; see the spine's rules section.

## calc_rule: cross-key and system-level derivation

A **calc_rule** reads datapoints and writes a datapoint (provenance **calculated**). It owns inputs, a reduce (worst / majority / average / Expr), an output key, and a scope. It is for **cross-key** and **system-level** derivation: a 5-minute average, a system rollup, `room.in_use` derived from display power + codec call-state + occupancy. Same-key multi-source reconcile is the key's `fusion_policy`, not a calc (see [Fusion](/architecture/datapoints/#fusion)).

The calculated value it writes is parallel to observed: both are machine-derived, distinguished by the **`provenance` column**, both carrying `source_rule` + `source_rule_version` on the row. See [calculated](/architecture/datapoints/#calculated-derived-by-a-calc-rule) for how the row records its lineage.

Calc folds **every** instance of an input key into the reduce: a rule reading `fan.speed` from a component gets one candidate per fan, so `worst` / `average` / `count` / Expr aggregate across all of them. Calc **outputs** stay aggregate (`instance = ''`); per-instance outputs are a deferred capability. See [the instance dimension](/architecture/datapoints/#the-instance-dimension-many-values-of-one-key-on-one-owner) for the full instance model.

Calc is one half of [fusion](/architecture/datapoints/#fusion): cross-key / system-level fusion is the only fusion that authors a rule (a `calc_rule`), deriving a higher-order fact (a new key) rather than reconciling one key across sources.

## The DAG invariant

Calc rules read observed and calculated values as truth; they never treat an intended value as truth to infer a new fact. That is what keeps the pipeline acyclic. The invariant is stated in full on [datapoints](/architecture/datapoints/#the-dag-invariant).

## Storage

The three rule families share one config shape, versioned so a backtest can pin the rule version; the physical layout lives on [storage](/architecture/storage/).

| Table | Key columns | Notes |
|---|---|---|
| `calc_rule` / `event_rule` / `action_rule` | **(id, version)**, scope, spec (jsonb: Expr + params) | config, named for function; versioned so a backtest can pin the rule version. `calc_rule` = cross-key/system-level derivation; `event_rule` = fire_criteria + optional clear_criteria ([events](/architecture/events/), [alarms and actions](/architecture/alarms-actions/)); `action_rule` = a subscription (an Expr predicate over events). Parsing is the edge function, not a rule; a deferred `discovery_rule` is not yet in schema |

Related: [datapoints](/architecture/datapoints/) (the data model calc reads and writes), [events](/architecture/events/) (the `event_rule`), [alarms and actions](/architecture/alarms-actions/) (the `action_rule` and the response layer), and [the glossary](/architecture/glossary/).
