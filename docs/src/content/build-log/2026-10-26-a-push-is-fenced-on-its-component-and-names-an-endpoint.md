---
title: "A push is fenced on the component it names, and its instance names an endpoint (#752, #456)"
date: 2026-10-26
refs: [752, 456]
---

`POST /telemetry:push` resolved its owner under the `telemetry:push` scope alone, so every refusal
was a 404, and a ruling recorded on #752 would have removed that fence altogether.
[ADR-0752](/architecture/decisions/#adr-0752-a-telemetry-push-is-fenced-by-telemetrypush-on-the-component-it-names)
keeps the fence and moves the route onto `ResolveActionTarget`, the split every other route acting
on a component target takes: outside the caller's `component:read` a non-disclosing 404, readable
but not pushable a truthful 403. `telemetry` resolves on the component tier alone, the
`command:issue` precedent, since a location- or system-tier grant could never reach a component's
chain. The matrix in `authz_matrix_test.go` drives the split through the route as `push-telemetry`.

The push lane now holds an instance to what ADR-0456 says it is: the endpoint an observation came
through. A push naming an instance no endpoint on its owner carries is refused whole with a 422 that
names the instance and lists the owner's endpoint names, at most ten and then a count, so a series no
observer stands behind is never written. The endpoint names are read once per batch, only when an
instance is named; `TestPushCostsOneScopeCheckPerBatch` counts statements at the pool and pins a
batch of forty-five observations to exactly the statements a batch of three issues. The
`reconcile-instances` demo now observes its display through two of its endpoints, and shows the
refusal first.
