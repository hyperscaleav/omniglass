---
title: "the settings Gateway is unscoped; only the permission gates it"
date: 2026-07-17
status: "Accepted"
summary: "Settings Gateway methods are unscoped: ABAC storage-scope is not applicable to platform / principal config; only the `settings:<action>` permission gates them"
---

- **Date:** 2026-07-17 | **Status:** Accepted | **Pages:** [settings](/architecture/settings/), [storage](/architecture/storage/), [identity and access](/architecture/identity-access/)
- **Decision:** The Storage Gateway methods for settings (`GetSettingOverrides`, `UpsertSettingOverride`,
  `DeleteSettingOverride`, `DeleteAllSettingOverrides`) are **unscoped**: no ABAC storage-scope predicate is
  injected. Only the `settings:<action>` permission at the route gates them (`settings:read` admin read with
  provenance, `settings:update` write / restore / lock, both admin-tier; the client-safe `/settings/me` is
  authn-only). This is a deliberate carve-out from the "scope on every applicable query" invariant, recorded so it
  reads as intentional.
- **Context:** The two authorization layers ([identity and access](/architecture/identity-access/)) are a
  `<resource>:<action>` permission on every route and an ABAC **scope** injected on every **applicable** query.
  Platform and cascade settings describe the **platform and its principals**, not the estate, so there is no
  location / system / component subtree to scope them by, exactly as with the registry-type reads
  (`GET /types/...`), which are also unscoped. Forcing a scope predicate here would be meaningless (there is
  nothing to filter on) and would misrepresent settings as estate data. The carve-out is narrow: it applies only
  because the data is platform config. When the group and user override rungs land, override reads and writes
  **will** be constrained by the acting principal (a user edits only their own `user` row), but that is a
  per-principal ownership check, a different mechanism than estate ABAC, not a return of tree scope.
- **Closes:** issue [#271](https://github.com/hyperscaleav/omniglass/issues/271) (settings engine slice-0).
- **Amended by [ADR-0057](#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier):** the model is unchanged, the level names are not: `code` is now `default` (off the axis, the setting's own declaration) and `global` is now `platform` (the install-wide rung).
