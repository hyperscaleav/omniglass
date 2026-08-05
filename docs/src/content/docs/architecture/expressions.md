---
title: Expressions
description: "Omniglass expressions: one engine built on Expr and extended with Omniglass functions, behind every operator-authored expression leaf."
sidebar:
  badge:
    text: Design
    variant: caution
---

:::design[Target design: the expression engine, tracked in #524]

Expressions let an operator reshape and judge collected values in plain text wherever the
platform needs a small computation, with exactly one language to learn: every site in the
table below goes through **one engine, Omniglass expressions**, built on **Expr**
([expr-lang/expr](https://github.com/expr-lang/expr)) and **extended** with Omniglass
functions.

## One engine, built on Expr and extended

**Expr** is the core, chosen because it is transform-oriented, fast, and sandboxable: a rich
built-in function and operator set suited to reshaping collected values (`raw / 100.0`,
`int(groups[1])`, `groups[2] == 'true'`), compiled to a fast program cached by
`(source, env-shape)`. On top ride **Omniglass functions**: helpers Expr does not ship,
including frame **`encode` / `decode`** and the output-format helpers (**hex / ascii /
base64**) that binary and raw-TCP protocols need. The engine is **not pluggable**: one dialect
everyone authors in; where an expression is not even needed, prefer a straightforward native
path.

## Unit conversion: `convert(value, "<unit>")`

Stored values are always in their `property_type`'s **canonical unit**; **`convert(value,
"<unit>")`** lets an operator author against a non-canonical one. The **source unit is
inferred** from the bound property's canonical unit; the **target** must be a registered unit
in the **same family** (a compile error otherwise). The conversion comes from the
[unit registry](/architecture/properties/#units-one-canonical-unit-per-metric): `to_canonical` /
`from_canonical` transforms, **affine** or an **Expr** for the rare nonlinear one. So
`convert(value, "fahrenheit") > 100` reads in Fahrenheit while storage stays in canonical
celsius. A function rather than a per-unit method: data-driven, general, available wherever
expressions run.

## Where expressions are used

| Site | Leaf | What it evaluates |
|---|---|---|
| extractor | `value` | reshape a located raw value into the typed sample value |
| step | `when` | the explicit branch guard (a false guard skips the step and dependents) |
| `event_rule` | `fire_criteria`, `clear_criteria` | open/close an alarm-paired event off a sample change |
| `calc_rule` | `reduce` (escape), `filter` | the named reducers (`worst` / `majority` / `average`, plus windowed `time_in_state` for SLIs) and the Expr escape, with per-input filters |
| rule | `scope` | which instances a rule fires for (the Expr scope escape) |
| views / list | `filter` | the structured-query predicate operators compose |
| dynamic group | membership `filter` | recomputed membership |

## In-scope bindings

Within a function run the engine environment exposes: `$var:<name>` (config/secret through the
cascade), `$prop.<key>` (properties, emitted and readable for branching; was `$dp.<key>`,
renamed with the ADR-0065 vocabulary), `$steps.<id>.*` (ephemeral scratch), `$event` (a listen
payload), and the
extractor-local inputs a step prepares for its `value` leaf (`raw`, `groups`, `node`, `item`).
Rule and view contexts bind their own documented environments (the candidate entity, the
property, the resource row).

## Divergence and `disagree(A, B)`

**`disagree(A, B)`** compares two provenances or sources of one key: config drift, reconcile
conflict, and cross-source disagreement are all the one operator, surfacing **divergence**,
the universal anomaly signal. Reading both sides as inputs (never writing a verdict back into
either) keeps the pipeline's DAG acyclic.

## Safety

Expressions are **sandboxed**: no I/O, no network, no unbounded loops, bounded execution.
Operator-supplied configuration values are bound as **data in the environment**, never spliced
into expression text, so a hostile value is evaluated literally and never executed. Secret
fields rendered into a request are masked at interpolation time and never surface in a log
line, error string, or property label.
:::
