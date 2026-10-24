---
title: "The systems list stops fetching health once per row"
date: 2026-08-13
refs: []
form: bullet
---

- **The systems list stops fetching health once per row**
  ([#653](https://github.com/hyperscaleav/omniglass/issues/653)). Every row of the systems list
  rendered a health badge that owned its own query, so a page of N systems fired **N HTTP requests on
  first paint**, and each one resolved every role the system needs, its occupants, their alarms and
  thirty days of recorded transitions in order to render one word and a colour. `staleTime` softened
  the refetch and did nothing for the first load, which is the one an operator waits on. Measured on a
  twelve-system page: **twelve requests before, one after.**

  The badge needed no change. It has always preferred a verdict the caller hands it and only fetches
  when it has none, and the fix is at the call site: the page reads verdicts once and passes them
  through that prop. What the call site does NOT do is also load-bearing. It passes the verdict and
  **not** the system id, because the id is what makes the badge fetch, and a cell that passed one while
  the bulk read was still in flight would put the per-row request back precisely where it was removed
  from. The column stays quiet until the map lands, which is the behaviour it already had.

  The read the issue named, `GET /views/estate` from
  [#632](https://github.com/hyperscaleav/omniglass/issues/632), is not on this branch's ancestry, so the
  other option the issue offers is the one taken: a narrower bulk read of exactly what the column
  renders. `GET /systems:health` answers one verdict per system in the caller's read scope, in **one
  statement whatever the estate size**, built on the `distinct on` pass over the property series that
  `locationVerdict` already ships rather than a correlated latest-row subquery per system. Its scope is
  `ListSystems`' scope by construction (the same `scopedListSQL` over the same table with the same
  binds), so a caller gets a verdict for exactly the rows it gets, and an empty scope is an empty list
  rather than a refusal.

  Two things are asserted rather than assumed, because both are places this could quietly go wrong. The
  bulk read computes nothing: it reads the recorded series and reports a system with no recorded row as
  healthy, so it is held to `SystemHealth`'s own live-computed verdict over a fixture carrying all three
  verdicts **and** a system nothing has ever recomputed, which is the row that default exists for. And
  the health column's freshness now depends on a second cache key, so the role and member writes that
  already invalidated the per-system key invalidate the bulk one alongside it; without that the column
  would go stale silently, which is exactly the regression #627's review round 3 caught in the same
  place.

  The visible UI is unchanged, deliberately: the same badges in the same column. The measurement is the
  request count, asserted in the page test against a twelve-row page loaded cold, with the per-row
  endpoint still served by the fixture so that the old implementation fails on the NUMBER rather than by
  rendering nothing.
