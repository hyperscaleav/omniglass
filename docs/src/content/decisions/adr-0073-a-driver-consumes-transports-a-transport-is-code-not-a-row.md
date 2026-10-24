---
title: "A driver consumes transports; a transport is code, not a row"
date: 2026-07-31
status: "Accepted"
summary: "a driver consumes transports; a transport is code, not a row"
---

- **Date:** 2026-07-31 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [glossary](/architecture/glossary/)
- **Decision:** The collect layer is **driver-centric**, closing the question
  [ADR-0039](#adr-0039-an-interface-is-a-device-api-the-interface-type-is-its-transport-not-its-driver) left open.
  Three things follow. **A transport is code, not an operator-editable row.** `interface_type` is a low-level
  primitive (`icmp`, `tcp`, `ssh`, `http`, `snmp`, ...), one per package, discovered through a code registry the way
  collection primitives already are; the `interface_type` **table**, its FK from `interface.type`, and the
  hand-written dispatch switch in `internal/node/probe.go` all retire together in a later slice. An operator cannot
  author a transport, because a transport is a wire implementation, not configuration. **A driver consumes
  transports and is never one.** A driver declares which transports it can run over and the instance picks one; the
  driver owns the normalized catalog (what to fetch, how to parse), the transport owns only how bytes move.
  **The `interface_type` is a driver clause in
  [ADR-0067](#adr-0067-bookings-are-exclusive-arc-owned-schedules-reconciled-against-observed-usage) is retracted**:
  a calendar integration is an `https` **transport** plus a `graph-calendar` or `google-calendar` **driver**, the
  same shape as every other device, not an `interface_type` named after a vendor API.
- **Context:** ADR-0039 recorded the driver-centric split as *"the current-best direction, not a locked gate"* and
  named its own successor: *"driver-centric vs template-centric is re-examined, and this ADR revised or superseded,
  in a later ADR before the collect layer is built."* This is that ADR, and it confirms rather than reverses the
  direction, because the alternative has been tried in this tree and lost twice. Protocol handling in the template
  makes every operator a programmer, which the product cannot ask for; and a transport as a **row** produced exactly
  the drift it invites, an ADR sourcing calendars through an `interface_type` named for a vendor API, which reads as
  reasonable precisely because a table accepts any string. A code registry cannot be extended by an operator, so it
  cannot silently absorb the driver's job. The estate model above this line is built (a component points at a
  product, a product carries a capability set and a property contract, roles staff a standard), while below it
  `driver` holds a name and a version and nothing a node could act on, so nothing is being unbuilt here: the
  decision authorizes work that the scope gate has been blocking.
- **Deliberately not decided here**, each with a home so none of it rides on this entry: whether a product may bind
  **more than one** driver (`product.driver_id` is a single nullable uuid today, and role-addressed driver output is
  the recommended alternative to a join table, but it is a recommendation, not a ruling); whether a product is a
  **versioned artifact instances pin** or stays the live classifier it is today
  ([#491](https://github.com/hyperscaleav/omniglass/issues/491)); where **cadence** lives; and how a component
  resolves its effective driver set (the `EffectiveCapabilities` plus-minus pattern in `internal/storage/roles.go`
  is the obvious candidate to reuse rather than invent a second mechanism).
- **Supersedes:** the status note on
  [ADR-0039](#adr-0039-an-interface-is-a-device-api-the-interface-type-is-its-transport-not-its-driver) (the
  driver-centric split is no longer provisional) and, in part,
  [ADR-0067](#adr-0067-bookings-are-exclusive-arc-owned-schedules-reconciled-against-observed-usage) (its calendar
  sourcing clause only; the booking entity and its arc ownership stand).
- **Enables:** the [collect layer epic](https://github.com/hyperscaleav/omniglass/issues/489), whose slices were
  unauthorized without it. Nothing in this entry is built; it is a decision, and the code lands in that epic.
