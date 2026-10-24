---
title: "Validate answers diagnostic codes, severities and fixes (#1052)"
date: 2026-10-23
refs: [1052]
---

The third slice of #1021. `POST /expressions:validate` answers every diagnostic with a `severity`
(`error`, `warning`, `hint`) and, from a registered rule, its `code` and a quick `fix` (a range and its
replacement); an expression that compiles answers `ok` with its warnings and hints (phase `lint`). A rule
an error diagnostic refuses is a 422 carrying the `code`, at create and update alike. Stored rules carry
their warnings as notes (definition decision 1): `criteria_warnings` on an event rule and
`calc_warnings` on a calc rule, read from the same cached compile that gives `criteria_error` and
`calc_error`, which gained the `code`; a warning never refuses a save, and a row that newly trips an
error diagnostic reads it as its fault without being rewritten. Hints stay in the editor. Proven by
`TestStoredRuleDiagnostics` (the gateway: the warning saved and read on get and list, the error refused at
create and update, the out-of-band row read as `sum-of-bools` and left as stored) and
`TestExpressionDiagnosticsAPI` (the route: each severity, the fix, the 422's code, both rules' warnings
on create and get).
