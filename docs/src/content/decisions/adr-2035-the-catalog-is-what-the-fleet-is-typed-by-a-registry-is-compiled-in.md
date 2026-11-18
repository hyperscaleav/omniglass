---
title: "The catalog is what the fleet is typed by; a registry is compiled in"
date: 2026-11-16
status: "Accepted"
summary: "Catalog names the database rows the fleet is typed by, at both levels (the catalog, the metric catalog) and on every surface; registry survives only for lookups compiled into the binary. Three catalog labels change to name the thing listed: Event rules, Severity levels, Notification targets"
refs: [2035, 2028, 2034, 2070]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [glossary](/architecture/glossary/),
  [UI](/architecture/ui/), [storage](/architecture/storage/)
- **Decision:** (#2035.) The line is where the thing lives. **Catalog** is a set of database rows
  the fleet is typed by, shipped or the operator's own: every list in the Catalog area and the
  area itself, at both levels ("the catalog", "the metric catalog", "a catalog row"). It is the
  word on every surface: the console, the docs, the API descriptions, the schema
  (`registry_shadow` becomes `catalog_shadow` in a new migration) and the Go names
  (`typeregistry.go`, `requireRegistryRow`, `registryTables` and the rest take catalog names).
  **Registry** survives only for a lookup compiled into the binary and resolved by name at
  runtime: the transports ([ADR-0073](/architecture/decisions/#adr-0073-a-driver-consumes-transports-a-transport-is-code-not-a-row)),
  the expression engine's functions, the simulator's session tables. It is never an operator
  word: `GET /transports` describes "the built-in transports", not "the code registry". In
  console and docs prose, registry joins the denylist; ADR titles and anchors keep it as history.
  Three catalog labels change so each names the thing listed, which the Actions group's
  neighbors made ambiguous: **Rules** becomes **Event rules** (its console route moves from
  `/rules` to `/event-rules`, the API's path), **Severity** becomes **Severity levels**, and
  **Targets** becomes **Notification targets**. The rest keep the pattern ADR-2034 set: a short
  label where the group header supplies the missing word (Types under Components, Systems and
  Locations), the bare noun for a signal catalog. The renames are a follow-up under #2028.
- **Context:** the two words were used interchangeably and at two zoom levels: the nav hint
  called the Catalog "every registry, browsable in one place", while each list was "the
  device-class genus registry" or "the property catalog". Counted (case-insensitive word
  hits): console 550 catalog against 456 registry, docs 944 against 827, Go 978 against 1,078
  across 126 non-test files, OpenAPI 120 against 46, generated CLI help 30 against 18,
  migrations 32 against 56, one table (`registry_shadow`), and no route or permission in
  either word. Read closely, nearly every Go use of registry (the storage and API packages)
  means the catalog tables, so the drift reached the code; only the transports, the expression
  functions and the simulator's sessions are compiled-in lookups. A two-level reading (a
  catalog is made of registries) was considered and rejected: nothing on screen behaves
  differently at the two levels, so the operator would learn a distinction with no use, and
  registry already carries the compiled-in sense. Peer usage agrees: a registry is what code
  resolves at runtime (container, schema and package registries), a catalog is what a person
  browses and picks from (a service or product catalog). The labels: "Rules" sits beside "Calc
  rules" and "Action rules" and reads as their parent while listing event rules alone (all three
  share the `rule` permission), and its route disagreed with the API; "Targets" sits in Actions,
  whose own copy gives commands "a target"; "Severity" names the attribute, not the ranked
  levels the page lists, the weakest of the three, changed for the rule that a label names what
  it lists.
