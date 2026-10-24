---
title: "Slice 1: the walking skeleton"
date: 2026-06-27
refs: []
form: bullet
---

- **Slice 1: the walking skeleton.** The single binary boots in two run modes. `omniglass migrate`
  applies an embedded [dbmate](/architecture/storage/) migration (pure DDL, run-once, idempotent) against
  a BYO Postgres, and `omniglass server` serves `GET /api/v1/healthz`, which pings the database through
  the [Storage Gateway](/architecture/storage/) seam (the only DB path, an interface from day one) and
  reports `{status, db}`. The [HTTP API](/architecture/api/) is Huma over chi; the OpenAPI 3.1 document
  is generated from the Go structs (`make gen`), the source the typed clients are generated from in later
  slices. Proven by a testcontainer integration test (migrate applies clean, the gateway ping succeeds,
  healthz returns db ok) and an end-to-end test that builds and runs the real binary. Deferred to later
  slices: messaging (NATS / JetStream), the worker and outbox, [identity and access](/architecture/identity-access/)
  (Slice 2), the [collection](/architecture/collection/) engine, and the rest of the generation pipeline.
