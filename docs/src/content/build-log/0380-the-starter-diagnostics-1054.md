---
title: "The starter diagnostics (#1054)"
date: 2026-10-23
refs: [1054]
---

The second slice of #1021, the registry's first rules, each in
`internal/expression/diagnostic_rules.go` with its fixtures beside it (compiled against criteria-shaped
and calc-shaped fixture environments that name their sites). `sum-of-bools` (refine, error) recognises
Expr's refusal of a sum over bools, whether the operator mismatch where the sum is compared or the
engine's own refusal of the list or predicate, and refuses on the `sum` with "sum adds numbers" and
the fix to `count`; `float-equality` (lint, warning) flags `==` or `!=` between a number-typed value and
a decimal that is not whole; `count-equals-len` (lint, hint) offers `all(xs, p)` for
`count(xs, p) == len(xs)`; `unused-alias` (lint, warning, calc only) names each input the reduce never
names, read through `WithInputs`, which a calc rule's environment now declares from its aliases. The
existing refusal of `sum(pts, .has)` moved from the operator to the `sum` with the clearer message.
Proven by the fixtures (`TestEveryDiagnosticPassesItsFixtures`, each fix applied and recompiled) and on
the real sites (`internal/rules/diagnostics_test.go`: the acceptance example refused at validate and
create alike and its fix compiling, the unused alias and the float equality never refusing).
