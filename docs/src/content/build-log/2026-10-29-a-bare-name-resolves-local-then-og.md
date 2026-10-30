---
title: "A bare catalog name resolves local, then og (#1432)"
date: 2026-10-29
refs: [1432, 318, 1307]
---

Slice 2 of namespaced catalog keys (#318,
[ADR-0318](/architecture/decisions/#adr-0318-every-catalog-key-is-a-namespace-and-a-key)). A
catalog's name is now unique within its namespace, not across them, on every catalog but
`severity_level`, so a release that ships a name an operator already holds writes its `og` row
beside the operator's `local` one instead of skipping it.

A bare name resolves along the search path: the operator's row first, then the platform's. In SQL it
is `<table>_in_effect()`; in Go, `storage.ResolveNameLane` for the three signal catalogs (one name
space across them, generalizing #1307's cross-catalog rule) and `storage.InEffect` for a list a
caller indexes by name. Every by-name read in the storage package, about 240 statements, now goes
through the search path, by uuid through any row, or into one named namespace;
`TestNoByNameLookupSkipsTheSearchPath` scans the package's SQL, constants and dynamic tables
included, and fails on a raw by-name read of a namespaced catalog.

The seed resolves a shipped row's references inside `og`, so shipped content always binds to
shipped rows: a product's vendor, driver, type and contract, a child type's parent, a command
type's target, an event rule's key and events, and a driver's validation. The seed upserts return
the row's id where the seed needs it back.

Two rulings: a role name both namespaces hold reaches neither (fail closed), and severity levels keep
names unique install-wide (overridden, never shadowed). An operator's own create or rename of a
`local` row onto a name `og` holds is refused as before, so a shadow arises only from a release.
