---
title: "The rule language's function set stops being typed four times"
date: 2026-08-13
refs: []
form: bullet
---

- **The rule language's function set stops being typed four times**
  ([#701](https://github.com/hyperscaleav/omniglass/issues/701)). Adding `words` in
  [#657](https://github.com/hyperscaleav/omniglass/issues/657) meant editing the `template.FuncMap`,
  `FuncNames()`, the AST allowlist and the prose that teaches the language, and forgetting any one of
  them failed differently: a name in the FuncMap alone parses nowhere, a name in the allowlist alone is
  just a hole, a name in `FuncNames` alone is published and unreachable, and a name missing from the
  docs simply does not exist as far as an operator is concerned. That is the generate-first drift class
  the repo's own rule names, inside the engine that renders the estate's labels.

  The set is now declared ONCE, as an ordered list of name, summary, worked example and the builder
  that produces the implementation, and the other four are readers of it. The safety property
  [ADR-0098](/architecture/decisions/) argues for is unchanged in what it guarantees and stronger in
  how: a function must be in the FuncMap AND the grammar to be usable, and deriving both from one
  declaration makes the two incapable of disagreeing rather than merely expected to. Adding a function
  is still a deliberate act with a test to change, because the closed-set test names the members and
  the committed artifact has to be regenerated.

  Two tests were owed rather than one. The old set was walked in ONE direction (every published name
  parses in a real rule), so a grammar entry with no implementation behind it was the direction nothing
  checked; the converse is now asserted too, and the allowlist may differ from the FuncMap only by the
  named builtins. The docs table is rendered from `docs/src/generated/label.json` (`cmd/labelgen`,
  drift-gated the way the seed and identity artifacts are), and each row's example is EXECUTED through
  the engine on the way into the artifact, so what the table teaches a function does is what the
  function did rather than a claim beside it.
