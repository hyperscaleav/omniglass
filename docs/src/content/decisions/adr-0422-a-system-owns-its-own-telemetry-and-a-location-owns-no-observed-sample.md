---
title: "A system owns its own telemetry, and a location owns no observed sample"
date: 2026-10-27
status: Accepted
summary: "A system owns metrics, properties and events of its own, written by a calc rule over its members (the primary path) or by a push naming it as the owner (`owner.kind: system` on `POST /telemetry:push`, fenced by `telemetry:push` on the system tier with ADR-0752's refusal split, one check per batch). A system has no endpoints, so a system push names no instance and carries no log line (each a 422), and a system's samples read on its own surfaces, its observed properties through a new `GET /systems/{name}/reconciliation`. Node-collected system telemetry is the shared-API fan-out of #1282, never a system-owned endpoint. A location owns no observed sample: the claim that location-level samples are first-class narrows to a location's calculated health"
refs: [422, 1282, 752, 456, 430]
---

- **Date:** 2026-10-27 | **Status:** Accepted | **Pages:** [metrics and properties](/architecture/properties/), [storage](/architecture/storage/), [core entities](/architecture/core-entities/), [collection](/architecture/collection/), [identity and access](/architecture/identity-access/), [api](/architecture/api/), [architecture](/architecture/)
- **Decision:** (#422, the architect's ruling: build it for systems, drop it for places.)
  1. **A system owns its own samples, from two producers.** A system calc rule over its members writes
     a system-owned calculated metric or property (#997), the primary path. A push names the system as
     its owner (`owner.kind: system` on `POST /telemetry:push`) for telemetry about the system as a
     whole, such as a cloud service's account of the room it serves. Events ride the same push: a
     system's caught event lands on its own arc and reads in its events. The ingest stamps the arc
     the binding resolved on every lane, never a hardcoded component.
  2. **The push fence joins the system tier.** `telemetry` resolves on the component and system tiers
     (ADR-0752's stated condition, with its test): a system owner is resolved through ADR-0116's split
     with `visible_set(P, telemetry, push)` as the action half and, push being the stronger grant, as
     enough to see the system; a miss resolves again with the caller's `system:read` only to choose
     the refusal, a non-disclosing 404 outside both or a truthful 403 when readable but not pushable.
     One check per batch. The tiers do not leak: push on a system confers nothing on its members, push
     on a member nothing on its system, and a location-tier grant reaches neither.
  3. **A system has no endpoints, so its push is owner-level and has no log lines.** An observation's
     instance is the endpoint it came through (ADR-0456), and a system has none, so a system push
     naming an instance is refused whole with a 422 (with no endpoint read). A log line is
     component-owned (#589), so a system batch carrying one is a 422 too, before anything resolves.
  4. **A system's samples read where its own reads are.** Its effective metrics carry an observed or
     calculated series, its effective properties a calculated one, a series' history reads on
     `/systems/{name}/metrics/{metric}/samples` and `/properties/{property}/samples`, and the new
     `GET /systems/{name}/reconciliation` pivots want, told and is for a system as the component's
     does, so an observed property reads beside its declared value. Every read is gated by
     `system:read` and scope-injected on the system tier.
  5. **Node-collected system telemetry is a fan-out, not a system-owned endpoint.** A component that
     speaks for many owners (a cloud or SaaS service modeled as a component) collects once, and each
     value lands on the owner its identity names through the value-to-owner index, one primitive for
     a node's poll and a webhook's post, extending the webhook ingress rather than duplicating it
     (#1282). An endpoint stays a component's.
  6. **A location owns no observed sample.** Nothing collects or pushes telemetry for a place: a
     push may not name a location, and the docs no longer claim location-level samples are
     first-class. The location arm of the sample arc stays for the one location-owned sample there
     is, its calculated health.
- **Context:** The docs called system-, location- and node-level samples first-class while the ingest
  hardcoded `owner_kind=component` on every lane and the push route refused any other owner kind, so
  an operator following the SaaS guidance ("telemetry of a room maps to system-owned samples") found
  no way to do it. Building it for both systems and places, as #422 first proposed, would have
  needed system- and location-owned endpoints: a new arc on `endpoint`, its addressing, its scope
  tiers, and an answer for every endpoint feature that assumes a component (commands, reachability,
  the webhook token). The architect ruled the narrower cut: a system's own telemetry is mostly
  calculated from its members, a push covers the rest today, and the collected case belongs to the
  fan-out the collection page already designs; a place has no telemetry of its own to collect.
