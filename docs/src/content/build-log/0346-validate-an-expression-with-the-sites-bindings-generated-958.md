---
title: "Validate an expression, with the site's bindings generated (#958)"
date: 2026-10-21
refs: [958]
---

The third slice of the expression engine (#524). Each place an expression is written is a **site**,
declared once in `internal/rules` (`site.go`): its environment per value type (for criteria, a
metric's number, a string or json property's text, a bool property's bool), the builtins it admits,
the result it must yield and its budget. `Env.Bindings` now lists a nested struct's fields by their
dotted path (`component.name`, `component.type`), the way an expression names them.

`POST /expressions:validate` (gated `rule:read`, `omniglass expression validate`) takes a site, a
source and, for criteria, the sample key (required; it types the value), and
answers whether it compiles, positioned diagnostics, and the site's bindings, builtins and budget,
read from the declaration. It stores and runs nothing; an unknown site or key is a 422, and the
site enum is checked against the registry by a test. `cmd/exprgen` (run by `make gen`) writes the
same declaration to `docs/src/generated/expressions.json`, which the expressions page renders as its
bindings and builtins table, so no binding is hand-listed; a drift test fails when the committed file
is stale. The expressions page moves from Design to Partial: the criteria site is built, and the
built engine (compiling once, the allowlist, the caps and budgets, the `$` refusal) is taught outside
the design fence. Validate compiles through `expression.Check`, which skips the shared program cache
so cycling sources evicts nothing, and a diagnostic names a nested binding by its path (`component`),
never by its Go type. A binding's doc reads the same for every value type, which a test holds.
