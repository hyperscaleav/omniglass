---
title: "Expression diagnostics: the registry (#1051)"
date: 2026-10-23
refs: [1051]
---

The first slice of #1021. The engine gains a registry of diagnostics
(`internal/expression/diagnostics.go`): each rule declares a stable code, a severity (error, warning or
hint), the sites it applies at, a trigger (refine, which explains a refusal it recognises at the place
the mistake is, or lint, which flags a likely mistake in an expression that compiles), a pure matcher
over the typed tree, a message, an optional fix as one text edit, and fixtures beside it in Go. To give
a refine rule the typed tree of an expression the checker refused, `compile` still goes through
`expr.Compile` (the engine never reimplements Expr's pipeline, by the architect's ruling in review) with
one more `expr.Patch` visitor that changes nothing and only records the slot the root sits in, read
after a refusal: Expr's checker annotates those same nodes in place, as far as it got
(`TestRefusedTreeKeepsCheckedTypes`). The recorder sits before the engine's charger, so the charger's
walk stays the last before Expr's final check, and `TestCompileGoesThroughExprCompile` fails if the
package imports Expr's checker or compiler. `Check` now returns the warnings and hints beside
its refusal, and a compiled program carries them (`Program.Diagnostics`). Each site names itself on its
environment (`WithSite`: criteria and calc), so a rule runs only where it applies. Proven with
test-local rules (`internal/expression/diagnostics_test.go`): a refine rule's refusal replaces Expr's
with its code and positioned fix, an unrecognised refusal is Expr's unchanged, warnings and hints ride
along on `Check` and on the cached program, an error lint refuses, a rule runs only at its sites, a
panicking matcher is skipped, the registry refuses a rule without both kinds of fixture, and the
fixture runner fails a rule whose examples disagree with it (`TestFixturesHoldARule`,
`TestEveryDiagnosticPassesItsFixtures`).
