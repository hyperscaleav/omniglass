---
title: "Catalog routes address a key as {namespace}/{key} (#1434)"
date: 2026-11-23
refs: [1434, 1433, 1457, 318]
---

Slice 4 of namespaced catalog keys (#318,
[ADR-0318](/architecture/decisions/#adr-0318-every-catalog-key-is-a-namespace-and-a-key)), shipped
in one rollup with #1433 (a telemetry sample names its namespace) and #1457 (a qualified sample is
written in its namespace exactly). Every catalog's item, child and custom-method routes move from
`/{name}` or `/{id}` to `/{namespace}/{key}`, so `og/x` and `local/x` are each reachable by their own
URL. This is a breaking change to every catalog route.

One primitive carries it: `gated` attaches `catalogKeyed` to any route whose path holds a
`{namespace}` segment, which documents the parameter and, after the permission check, resolves each
pair through `ResolveCatalogKey` into the ref that reaches exactly that row (its name where the
search path reaches it, else its uuid). No handler changed its storage call. A uuid key answers only
under its row's namespace. Catalog read bodies carry `namespace`; a create lands in `local` and
refuses `og` (422). The CLI generator fuses each pair into one positional, `ns/key` or a bare name
resolved local then og.

Tests: `TestEachNamespaceOfANameIsReachableByItsOwnURL`, `TestAUUIDKeyAnswersOnlyUnderItsRowsNamespace`,
`TestAChildRouteOnAQualifiedParentReadsItsOwnRows`, `TestACreateLandsInLocalAndNeverInOg`
(`internal/api`), and `TestACatalogPositionalTakesNamespaceKeyOrABareName` (`internal/cli`).
