---
title: "The generated diagnostics table (#1055)"
date: 2026-10-23
refs: [1055]
---

The fourth slice of #1021. `make gen` (`cmd/exprgen`) renders the diagnostics registry to
`docs/src/generated/expression-diagnostics.json` (`expression.DiagnosticsFactsJSON`), and the expressions
page renders it as a table (`ExpressionDiagnostics.astro`): each rule's code, severity and when it runs,
its sites, its message, and its examples, which are its fixtures (what it flags, with the fix's result,
and what it leaves alone), so no row or example is hand-written. Each row carries the anchor
`#diagnostic-<code>` the console's editor links a code to. Proven by `TestDiagnosticsFacts` (every rule,
its examples its fixtures) and the drift gate `TestDiagnosticsFactsMatchCommitted`.
