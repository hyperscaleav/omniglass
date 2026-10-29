---
title: "A driver function reads records and names each one's identity (#1386)"
date: 2026-10-29
refs: [1386, 1282]
---

Slice 2 of the shared-API fan-out (#1282,
[ADR-1282](/architecture/decisions/#adr-1282-a-conduit-fans-each-identified-value-out-to-the-owner-its-binding-names-and-a-binding-costs-a-write-on-both-of-its-ends)).
A poll or a listener may now declare `records`, a dotted path to an array in a JSON body, and an
`identity`, a registered property type and the extract that reads its value. The one engine reads
each array element as its own payload, so one poll of a room list, or one batched webhook post,
yields one identified record per room.

Every metric, property and event a record yields carries its identity onto the wire: the
`og.v1.TelemetryBatch` metric, property and event samples gained an `identity` field (the match key
and the value), set by the node's batch builder for a polled or a held-session listener and by the
webhook's batch for a post. A record whose identity misses lands nothing and is one fault naming
the record. Log lines never carry an identity. The driver preview shows each sample's identity.

At write, the key must be a registered property type a driver may write, the identity's extract is
held to the transport as an emit's is, and `records` is refused on anything but a JSON body. A
function that declares neither field bakes to the bytes it always did:
`TestAFunctionWithoutIdentityKeepsItsTaskHash` pins a task's hash and spec captured before the
fields existed.

Nothing resolves an identity to an owner yet, so an identified sample still lands on the conduit's
component with its instance unchanged. The fan-out step that lands it on the bound owner is slice 4.
