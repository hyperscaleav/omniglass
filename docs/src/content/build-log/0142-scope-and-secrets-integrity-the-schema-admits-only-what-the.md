---
title: "Scope and secrets integrity: the schema admits only what the model allows ([#431](https://github.com/hyperscaleav/omniglass/issues/431))"
date: 2026-08-01
refs: [431]
form: bullet
---

- **Scope and secrets integrity: the schema admits only what the model allows
  ([#431](https://github.com/hyperscaleav/omniglass/issues/431)).** The unifying idea, a value the code refuses must
  not be a value the schema, the API, or the console offers, closed across eight slices. The secret directory now
  **injects scope into its query** through the new arc-scope primitive (`arcScopeCTEs` / `arcScopePredicate`, the
  exclusive-arc counterpart of the scoped-CRUD walk; one query regardless of row count), retiring the audit's
  `unscoped-gateway-list` anti-pattern; the secret **system band** is gone from the CHECK, the enum, the console
  picker, and the docs (finishing ADR-0052); the sample provenance domains lost their impossible `declared` arm;
  `scope_kind='group'` stopped being advertised by the schema and both grant enums (group-as-scope returns with its
  slice); the decoy `platform_setting` table and the eight unrouted seed grants are gone, the ship-ahead
  allowlist now deliberately empty under a reversed policy (a grant lands with its first route); the Helm chart
  defaults session cookies to `Secure`; and `alarm` gained its **condition identity**
  ([ADR-0075](/architecture/decisions/#adr-0075-an-alarms-condition-identity-is-a-raiser-supplied-dedup-key)): a
  `dedup_key` plus the `alarm_open_condition_key` partial unique index make one-open-per-condition a database fact,
  with `RaiseAlarm` a guarded conditional insert returning the existing incident instead of a duplicate. Run as the
  second feature loop on the ADR-0074 machinery.
