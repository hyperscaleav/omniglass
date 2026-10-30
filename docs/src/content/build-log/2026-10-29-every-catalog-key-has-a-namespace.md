---
title: "Every catalog key has a namespace, and the seed writes only og (#1431)"
date: 2026-10-29
refs: [1431, 318]
---

Slice 1 of namespaced catalog keys (#318,
[ADR-0318](/architecture/decisions/#adr-0318-every-catalog-key-is-a-namespace-and-a-key)). The 18
catalogs that carry `official` gain a `namespace`: `og` for the platform's rows, `local` for the
operator's, backfilled from `official`. A CHECK holds that an `og` row is official.

The boot seed now lands every shipped row in `og`, and each of its 16 upserts applies its conflict
update only to an `og` row. Before this, 14 of them took an operator's row of a shipped name over
and two (notification types and severity levels) refused and stopped the boot; an official event
rule refused the same way. Now the operator's row is left exactly as it was, the shipped row's
children (a product's contract) are not written either, and the server boots. One refusal stays: a
severity rank a differently named operator level holds still stops the boot, naming it, until #1432
settles ranks under namespaces. An official rule or driver meeting an operator's of the same name is
left unprepared, so it is never compiled against a catalog the operator holds differently.

`TestEveryOfficialCatalogIsNamespacedAndTheSeedWritesOnlyOg` enumerates the catalogs from
`information_schema`, so a catalog added later is held to the rule without being listed: it turns
every shipped row into an operator's, seeds again, and requires each to read back unchanged.

Uniqueness stays on `name` for now, so on an install where an operator already holds a shipped
name, the shipped row is not written, and a shipped row that names it binds to the operator's. The
search path (#1432) lets both exist and resolves the seed's references inside `og`.
