---
title: "A component that owns endpoints cannot be deleted (#138)"
date: 2026-11-11
refs: [138, 114, 528]
---

Deleting a component refused only while it had child components. A childless component that still
owned endpoints deleted cleanly, and the foreign key set each endpoint's `component` to null, which
left the endpoint, and every task derived from it, reachable by an all-scope caller only.

The component's delete now also counts the endpoints that name it, and a nonzero count is a 409 that
says how many and what to do: "component still owns 2 endpoints: delete or reassign its endpoints
first". The refusal is its own error (`ErrComponentHasEndpoints`, carrying the count) rather than a
widening of `ErrComponentOccupied`, whose text says child components. The child-component refusal and
the referenced-record 409 are unchanged, and the delete route's description names all three.

The one in-tree caller that deleted a component with endpoints, the rollback of a failed
`POST /nodes/{name}/simulation/components`, now deletes the endpoints it already made before the
component.

Tests on a real Postgres: the gateway refuses a component with one endpoint, leaves the endpoint's
component set, and deletes the same component once the endpoint is gone; the API answers 409 naming
the endpoint; and `omniglass component delete` exits non-zero printing that detail.
