---
title: "The Alarms page reads cleared alarms and how long each lasted (#2426)"
date: 2026-11-23
refs: [2426, 2333, 2425]
---

`GET /alarms` takes `state` (`active`, the default, `cleared` or `all`) and `since` / `until` (RFC 3339),
bounding the overlap of an alarm's raised-to-cleared interval with the range: raised at or before `until` and
still active or cleared at or after `since`, so one raised before the range and open in it is kept; `since`
after `until` is a 400. Permission and scope are unchanged, and the keyset cursor pages a history the same way
it pages the queue. The same names and semantics as the component and system alarm reads. Rows gained
`system_ids`, parallel to `systems`, so the console links the one system rather than a name two locations
share, and a component placed only through its system is now named at that system's location instead of a
blank (`attachSystemLocations`: one query and one batched path walk for the page).

The page gained an Open / Cleared / All control held in the address (`?state=`), Cleared and Duration columns
(Duration counts up for an open alarm and is sortable), a cleared row opening the same drawer with `Lasted`,
and a cleared row of a component's Recently cleared list and a system's Other alarms reading `lasted 12m` from
the one `durationText`. The lifecycle walk's cell fixes ride along: System and Location cells link, the acked
badge says who acknowledged, and the snooze pill keeps its words on one line.

The three-way control is the `SegmentedControl` wrapper from `claude/feat-primitive-wrappers` (#2485), taken
as the one file (and its test) so the PR does not carry that branch's migration; it merges cleanly when that
branch lands. No migration, no index: the history reads the same `alarm` table the queue reads.
