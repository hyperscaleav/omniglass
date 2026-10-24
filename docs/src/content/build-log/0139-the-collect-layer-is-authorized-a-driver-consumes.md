---
title: "The collect layer is authorized: a driver consumes transports, and a transport is code ([ADR-0073](/architecture/decisions/#adr-0073-a-driver-consumes-transports-a-transport-is-code-not-a-row))"
date: 2026-07-31
refs: []
form: bullet
---

- **The collect layer is authorized: a driver consumes transports, and a transport is code
  ([ADR-0073](/architecture/decisions/#adr-0073-a-driver-consumes-transports-a-transport-is-code-not-a-row)).**
  [ADR-0039](/architecture/decisions/#adr-0039-an-interface-is-a-device-api-the-interface-type-is-its-transport-not-its-driver)
  recorded the driver-centric split as "the current-best direction, **not a locked gate**" and named its own
  successor: driver-centric versus template-centric had to be re-examined "in a later ADR **before the collect layer
  is built**". That ADR was never written, so every collect slice sat behind the scope gate. This is it, and it
  confirms rather than reverses: `interface_type` is the **transport**, decided to be a **code** registry (one
  package per transport) rather than an operator-editable table, which retires the table, its FK from
  `interface.type`, and the hand-written dispatch switch in `internal/node/probe.go` in a later slice. A **driver**
  declares the transports it can run over and is never one of them. The `interface_type` **is** a driver clause in
  ADR-0067 is **retracted**: a calendar integration is an `https` transport plus a `graph-calendar` driver, which is
  the drift a table invites, since any string fits where a transport belongs. Two denylist entries land with it,
  matching the **claim** rather than the two nouns, so ordinary prose naming both a transport and a driver stays
  correct. Nothing here is built: `driver` still carries identity only (name, version, official), and the catalog,
  the normalized menu, discovery, and the device pack are the [collect layer
  epic](https://github.com/hyperscaleav/omniglass/issues/489). Deliberately left open, each with a home: whether a
  product may bind more than one driver, whether a product is versioned or stays a live classifier
  ([#491](https://github.com/hyperscaleav/omniglass/issues/491)), and where cadence lives.
