---
title: "Event rule criteria are expressions (#957)"
date: 2026-10-21
refs: [957]
---

The second slice of the expression engine (#524): an event rule's `fire_criteria` and
`clear_criteria` are expressions, stored as `{"expr": "..."}` and compiled by the criteria site in
`internal/rules` (`rules.Compile`, a `Judge` per rule). The prepared input binds `value` (a metric's
float64; a property's value typed by its `data_type`, text for `string` and `json`, a bool for
`bool`), `key`, `instance` and `component {name, type}`, with the criteria builtins and the criteria
budget, result bool. The short form `{op, value}` is still accepted on input (the API, the CLI, the
console's builder) and lowered by a pure `rules.Lower`: text JSON-quoted (`value == "down"`), a number
made a float literal (`value >= 80.0`) so Expr parses it with the same correctly rounded ParseFloat,
and a bool property's non-boolean threshold lowered to the constant it always meant. An absent clear is
still `!(fire)`; a non-finite metric, or a `bool` property reading neither `true` nor `false`, holds.
That last is the lowering's one divergence from the old evaluator, by decision (decision 3 on #524):
such a sample was compared as text before and holds now.

A one-time data backfill (`20261021040000`, no DDL) rewrote every stored short form with the same
lowering in SQL, byte for byte: numbers are written as Postgres writes a jsonb number (plain decimal,
no exponent) and text is escaped as `to_json` escapes it, in both, and the integration test compares
the text (exponent forms and U+2028 included). A row whose expression would pass the 4096-byte source
cap is left in short form rather than failing the migration. Its down migration restores the short
form for every expression the lowering writes. The old `{op,value}` evaluator is kept as a test
oracle: the lowering decides 21,324 criterion and sample pairs as it did (hostile strings, quotes,
control characters, unicode, signed zero, NaN, the edges of float precision), the bool divergence
pinned separately, and an integration test migrates short-form rules forward, back and forward again,
re-seeds, and drives an upgraded reachability rule and a metric rule through the real ingest. The official `reachability-lost` ships the backfill's own text, so an
upgraded fleet's first boot changes nothing.

The gateway holds each rule's compiled criteria per (rule, version, data type), so the ingest, the
prime, the sweep, the reconcile scan and its settle all judge with a program compiled once per rule
version (a test counts zero compiles across two 50-sample batches). A compile that fails is cached the
same way, and the rule carries it as `criteria_error` (field, line, column, message) on its list entry,
get and state, which the Rules list and blade show as criteria that do not compile; that is how a rule
whose key's data type changed under it, or a short form the backfill left over the cap, is seen. A run
over its budget, or a value the lane cannot read, holds the rule for that sample, is logged once per
rule version per process, and the sample still lands. The API accepts `{expr}` or `{op, value}` and always answers `{expr}`; a criterion that
does not compile is a 422 located at `body.fire_criteria.expr` (or `clear_criteria`), or at the
criteria when the body gave the short form, with its line and column. The console's builder still sends the short form; the Rules page reads a lowered expression
back into its comparison for the words and the form, and shows any other expression as written,
keeping it on save (editing it in the console is #960).
