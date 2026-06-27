---
title: Expressions
description: "Omniglass expressions: one engine built on Expr and extended with Omniglass functions, behind every operator-authored expression leaf."
sidebar:
  badge:
    text: Design
    variant: caution
---

Expressions let an operator reshape and judge collected values in plain text wherever the platform needs a small computation, and there is exactly one language to learn for all of them. Omniglass evaluates these small operator-authored expressions in many places: an extractor's
`value` leaf, a step's `when` guard, an `event_rule`'s fire/clear
criteria, a `calc_rule`'s reduce escape, a rule's `scope` predicate, a view/list `filter`,
and a dynamic group's membership filter. All of these go through **one engine, Omniglass
expressions**, built on **Expr** ([expr-lang/expr](https://github.com/expr-lang/expr)) and
**extended** with Omniglass functions.

## One engine, built on Expr and extended

There is one expression engine. It is **Expr** at the core, chosen because it is
transform-oriented, fast, and sandboxable: it is expression-oriented with a rich built-in
function and operator set well suited to reshaping collected values (arithmetic, string ops,
slicing, mapping over arrays, null handling, things collection extractors do constantly like
`raw / 100.0`, `int(groups[1])`, `node.gain`, `groups[2] == 'true'`), it compiles to a fast
program, and it is straightforward to sandbox.

On top of that base we add **Omniglass functions**: helpers the platform needs that Expr does
not ship, including frame **`encode` / `decode`** and the output-format helpers (**hex /
ascii / base64**) that binary and raw-TCP protocols need to pack and unpack wire bytes. The
engine is **not pluggable**: there is one dialect everyone authors in, and a compiled program
is cached by `(source, env-shape)` so compile cost is paid once. Keeping it to one engine is
deliberate (YAGNI on multiple engines); where an expression is not even needed, prefer a
straightforward native path over reaching for the engine at all.

## Unit conversion: `convert(value, "<unit>")`

Stored values are always in their `datapoint_type`'s **canonical unit**, so an operator who
wants to author against a non-canonical unit converts at the expression. **`convert(value,
"<unit>")`** is the stdlib function for this: the **source unit is inferred** from the bound
datapoint's canonical unit, and the **target** is a registered unit that must be in the
**same family** (a compile error otherwise, since units only convert within one dimension).
The conversion itself comes from the [unit registry](/architecture/datapoints/#units-one-canonical-unit-per-key): the target's
`to_canonical` and `from_canonical` transforms, **affine** (a factor plus offset) for the
common case or an **Expr** for the rare nonlinear one. So an operator can write
`convert(value, "fahrenheit") > 100` while storage stays in canonical celsius: the threshold
reads in Fahrenheit, the firehose never changes unit. The function form is chosen over a
per-unit method like `value.toFahrenheit()` (which would need a method per unit); it is
data-driven and general, available wherever expressions run, including `event_rule` /
`alarm` criteria, `calc_rule` leaves, and view/list filters.

## Where expressions are used

| Site | Leaf | What it evaluates |
|---|---|---|
| extractor | `value` | reshape a located raw value into the typed datapoint value |
| step | `when` | the explicit branch guard (a false guard skips the step and dependents) |
| `event_rule` | `fire_criteria`, `clear_criteria` | open/close an alarm-paired event off a datapoint change |
| `calc_rule` | `reduce` (escape), `filter` | the named reducers (`worst` / `majority` / `average`, plus windowed `time_in_state` for SLIs) and the Expr escape, with per-input filters |
| rule | `scope` | which instances a rule fires for (the Expr scope escape) |
| views / list | `filter` | the structured-query predicate operators compose |
| dynamic group | membership `filter` | recomputed membership |

Because `filter` is the same engine everywhere, an operator who can write a group filter can
write a list filter and a rule scope. One language across the surface.

## In-scope bindings

Within a function run the engine environment exposes the documented namespaces: `$var:<key>`
(config/secret through the cascade), `$dp.<key>` (datapoints, emitted and readable for
branching), `$steps.<id>.*` (ephemeral scratch), `$event` (a listen payload), and the
extractor-local inputs a step prepares for its `value` leaf (`raw`, `groups`, `node`,
`item`). Rule and view contexts bind their own documented environments (the candidate
entity, the datapoint, the resource row).

## Safety

Expressions are **sandboxed**: no I/O, no network, no unbounded loops, bounded execution.
Operator-supplied configuration values are bound as **data in the environment**, never spliced
into expression text, so a hostile value is evaluated literally and never executed. Secret
fields rendered into a request are masked at interpolation time and never surface in a log
line, error string, or datapoint label.
