---
title: "Collection slice (checkpoint 5a): the reachability verdict as a state"
date: 2026-07-19
refs: []
form: bullet
---

- **Collection slice (checkpoint 5a): the reachability verdict as a state.** The first honest substrate for
  availability history. The node now computes the per-interface verdict `interface.reachable` (`up`/`down`, the
  AND of the interface's probe results) and emits it as a built-in **state** datapoint (seeded `datapoint_type`
  at `kind=state`, domain `up`/`down`), riding the proto `string_value`. A new `state_datapoint` table mirrors
  `metric_datapoint` (same owner exclusive-arc, same lineage CHECK) with a categorical `value text` plus optional
  `value_json`, and the Gateway gains `InsertStateDatapoints` / `LatestState` / `StateTransitions`. The ingest
  consumer now **routes by the registry kind** (metric to `metric_datapoint`, state to `state_datapoint`) after
  the **unchanged** owner-confinement and reject-not-project, so a foreign or unregistered state is orphan-dropped
  identically to a metric (negatively tested for the state path). The series is **transition-only**: the node
  remembers the last verdict per interface and emits only on a flip or first observation, and the ingest side
  re-guards by skipping a write whose value equals the latest stored value (the net for a node restart), proven
  by a negative test that repeated identical verdicts produce one row per transition, not per tick. Availability
  as `time_in_state` over this state, and the operator surfaces that render the transitions, are checkpoint 5b.
  See [ADR-0034](/architecture/decisions/#adr-0038-the-reachability-verdict-is-a-built-in-state).
