---
title: "A telemetry sample names its namespace (#1433)"
date: 2026-10-30
refs: [1433, 318]
---

Slice 3 of namespaced catalog keys (#318,
[ADR-0318](/architecture/decisions/#adr-0318-every-catalog-key-is-a-namespace-and-a-key)). The
metric, property and event samples on `og.v1.TelemetryBatch` gain `namespace`, as do the items of
`POST /telemetry:push`. Set, the name is read in that namespace only: `og` reaches the platform's
row while an operator's row of the same name shadows it, and a namespace holding no such row
refuses the sample. Empty walks the search path, so every existing producer is unchanged.

The ingest registry keeps the row each qualified name resolves to beside its bare maps, and a
sample whose qualified row is the one its bare name reaches is written bare. Only a sample onto a
row the search path does not reach carries its namespace to the Storage Gateway, which reads the
type there, holds the row to that row's writer class, and lets no event rule, calc rule or command
settlement judge it, since each addresses a key by its bare name. A push rejection names the
namespace it searched, and a shadowed name's reason now says naming `og` reaches the official row.

A driver emit naming its namespace, which carries this to the node and webhook lanes, is its own
cut (#1456).
