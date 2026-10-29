---
title: "A conduit fans each identified value out to the owner its binding names, and a binding costs a write on both of its ends"
date: 2026-10-29
status: Accepted
summary: "A component that speaks for many owners (the conduit) collects once, and each value a driver function identifies (a match key, a registered property type, and its value) lands on the one system or component an `identity_binding` row ties to `(conduit, key, value)`, through one fan-out step the node lane and the webhook lane both pass. A binding is made under the conduit (`/components/{name}/identity-bindings/{key}/{value}`) and costs `component:update` on the conduit and the owner's update (ADR-0510's rule), so nothing in a payload names an owner: it names an identity, resolved only against the bindings of the conduit the batch is already confined to. A fanned sample is observed, collected-only, at the empty instance, its conduit named in `source` as `<component>/<endpoint>` (amending ADR-0456's observer stamp); an identity nobody bound lands on no owner and is kept in the conduit's bounded `unbound_identity` inbox (amending ADR-1024 decision 7). The collection page's cascade-scoped index and observed-identity fallback are deferred"
refs: [1282, 456, 1024, 510, 422, 161]
---

- **Date:** 2026-10-29 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [metrics and properties](/architecture/properties/), [identity and access](/architecture/identity-access/), [glossary](/architecture/glossary/), [drivers](/guides/admin/drivers/)
- **Decision:** (#1282, the architect's approval of 2026-10-29, every open decision as recommended.)
  1. **The conduit and its identities.** A driver function may declare an `identity` (a match key,
     which is a registered `property_type`, and the extract that reads its value) and `records` (a
     JSON selector for an array, so its emits and its identity read once per record). The component
     whose endpoint ran the function is the **conduit**: the path, not the owner. Every sample an
     identified record yields carries its identity on the wire; a record whose identity misses lands
     nothing and is a fault. Log emits carry no identity and stay on the conduit (a system owns no
     log line, ADR-0422).
  2. **What authorizes a fanned write: a two-ended binding** (decision 1). An `identity_binding` ties
     `(conduit component, match key, value)` to exactly one owner, a system or a component (a
     location or a node is refused, ADR-0422). Writing one costs `component:update` on the conduit
     and `component:update` or `system:update` on the owner, the
     [ADR-0510](#adr-0510-a-membership-or-a-role-assignment-costs-a-write-on-both-of-its-ends) rule:
     an owner-side identity alone would let a principal scoped to one system declare another room's
     id and siphon a conduit it cannot read, and a conduit-side binding alone would let a principal
     scoped to the conduit write onto systems it cannot update. Moving a binding to another owner
     costs update on the owner it leaves as well. Consent is given at write: a binding persists if
     its author later loses scope, as a membership does, and deleting either end deletes it.
  3. **Where it lives** (decision 2). Under the conduit,
     `/components/{name}/identity-bindings/{key}/{value}`, beside the unbound inbox it is bound from;
     an owner-side read is later. The list names an owner only inside the caller's read on its tier
     and counts the rest.
  4. **The match key is a registered property type** (decision 3), so two drivers reporting one key
     share it, and a key is validated where a spec declares it.
  5. **One fan-out step, confinement first.** The ingest consumer resolves every identified sample in
     a batch against the conduit's bindings in one read and lands the batch in its one transaction
     under its receipt (ADR-0311). On the node lane the conduit is the component `ResolveTaskOwner`
     already confines the node to, so a batch confinement orphans is dropped before any identity is
     read; on the webhook lane it is the component the token's one endpoint names. The fan-out read
     runs in the ingest's system mode: the binding is the fence, checked when it was written.
  6. **What a fanned sample is** (decision 4). It is observed, admitted by the Storage Gateway only
     for a `collected` key on either lane
     ([ADR-0161](#adr-0161-every-key-has-one-writer-class-held-at-the-storage-gateway)), held to an
     owner the conduit's bindings name, and it lands once, on its owner and not also on the conduit.
     It lands at the **empty instance** and names its observer in `source` as
     `<conduit component>/<endpoint>`. This amends
     [ADR-0456](#adr-0456-an-instance-names-which-value-of-one-key-on-one-owner-a-row-is-and-an-owner-level-read-reduces-across-every-instance)'s
     observer stamp: the conduit's endpoint is no endpoint of the owner (a system has none), so as
     an instance it would read as a retired observer and hide the value from the owner's own reads.
     Two conduits reporting one key on one owner stay told apart by `source`, as
     [fusion](/architecture/properties/#fusion) tells perspectives apart. The cost is stated: an
     owner-level property fed by two conduits is one transition-only series, so a flip between their
     values writes a row each time, revisited if a consumer needs a series per conduit.
  7. **Unmatched values are kept** (decision 5). An identity the conduit has no binding for lands on
     no owner and is upserted into `unbound_identity` (one row per conduit, key and value: a count,
     first and last heard, the keys it carried; 1024 rows per conduit, the least recently heard
     evicted; aged out after seven days), read under `component:read` on the conduit. A webhook
     post's raw log line still lands on the conduit. The ingest warns once per conduit per window,
     never per sample. This amends
     [ADR-1024](#adr-1024-omniglass-receives-webhooks-on-the-server-and-a-webhook-token-is-a-sealed-secret)
     decision 7: the payload selects only among the owners bound to the token's component.
  8. **Deferred** (decision 6). The collection page's cascade-scoped index (an identity declared at a
     location or system binding every member below it) and its fallback to an observed identity
     sample are not built: each widens who can place a value on an owner without the conduit end's
     consent, so each needs its own fence and a consumer to justify it. The page and the glossary
     describe what ships.
  9. **The lab** (decision 7). The multi-room simulated UCC service is added beside the per-room
     instances and bound in the dev seed; their retirement is #1390.
- **Context:** A system owned its own telemetry only through a push or a calc rule (ADR-0422), and a
  webhook endpoint landed every post on its own component, so a cloud service reporting on many rooms
  either misfiled every room's value on itself or needed a component per room. The collection page
  designed a value-to-owner index declared on the owner and resolved for any source; written that
  way it was a door around scope, since whoever could write an owner's identity could pull another
  conduit's values onto it. The binding with two ends closes both sides, reuses the membership
  precedent, and fits #1367's passive gear, whose match key is a feeder plus a channel.
