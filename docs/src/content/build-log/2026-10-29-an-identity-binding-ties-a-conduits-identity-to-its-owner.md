---
title: "An identity binding ties a conduit's identity to the owner it names (#1385)"
date: 2026-10-29
refs: [1385, 1282, 510]
---

The first slice of #1282 stores the binding the fan-out will resolve through. An `identity_binding`
row ties one value a conduit component reports under a match key (a registered property type; the
boot seed gains `ucc-room-id`) to exactly one owner, a system or a component, on an exclusive owner
arc whose CHECK admits no place and no node. The Storage Gateway lists, puts and deletes bindings with
both ends scoped, the [ADR-0510](/architecture/decisions/#adr-0510-a-membership-or-a-role-assignment-costs-a-write-on-both-of-its-ends)
rule applied to a relationship whose owner may sit on either tier: the conduit resolves in
`component:read` then `component:update`, the owner in its own tier's pair, a move also needs update
on the owner it leaves, and a re-put to the same owner writes nothing. Every change is audited in its
transaction, naming both ends. The routes live under the conduit,
`GET /components/{name}/identity-bindings` and `PUT` and `DELETE` on
`/components/{name}/identity-bindings/{key}/{value}`, with the owner's `system:update` published as
the conditional permission and refused with a 403 naming it before anything resolves; the list names
an owner only inside the caller's read on its tier and counts the rest. The CLI
(`omniglass component identity-binding`) and three MCP tools are generated from them. The
[collection](/architecture/collection/#identity-binding-a-conduits-identity-tied-to-its-owner) page
renders the table and describes the binding as built, with the fan-out itself still behind its design
fence until slice 4
([ADR-1282](/architecture/decisions/#adr-1282-a-conduit-fans-each-identified-value-out-to-the-owner-its-binding-names-and-a-binding-costs-a-write-on-both-of-its-ends)).
