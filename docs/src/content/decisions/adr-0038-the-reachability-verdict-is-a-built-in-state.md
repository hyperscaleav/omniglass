---
title: "The reachability verdict is a built-in state"
date: 2026-07-07
status: "Accepted"
summary: "The per-interface reachability verdict `interface.reachable` is a built-in **state** (not a metric); availability is `time_in_state` over it; readiness is interface-type-defaulted and interface-overridable, node-executed, not a `calc_rule`"
---

- **Date:** 2026-07-07 | **Status:** Accepted | **Pages:** [datapoints](/architecture/properties/), [collection](/architecture/collection/)
- **Decision:** The per-interface reachability verdict `interface.reachable` (value domain `up` / `down`) is a
  first-class **state** datapoint, not a metric, seeded as an official `datapoint_type` at `kind=state`,
  `value_type=text`, `validation: {values:[up,down]}`. It is gated **per interface**: the verdict is the AND of
  that interface's applicable probe results (for the inline tcp/icmp interfaces this is degenerate, one probe
  drives the verdict; it generalizes to an interface with several probes). The **node** computes it after running
  the interface's probe(s) and emits it as an `observed` state datapoint instanced by the interface; the ingest
  consumer **routes by the registry kind** (a metric name to `metric_datapoint`, a state name to
  `state_datapoint`) after the same owner-confinement and reject-not-project, so a foreign or unregistered state
  is dropped identically to a metric. The series is **transition-only**: the node remembers the last verdict per
  interface and emits only on a flip or first observation, and the ingest side re-guards by skipping a write whose
  value equals the latest stored value (the net for a node restart). Availability is `time_in_state` over this
  state (health's primitive one tier down), a later slice; the raw probe metrics (`tcp.open`, `icmp.reachable`,
  the rtts) keep emitting unchanged. Readiness config (an ssh command + regex, an snmp OID) is an
  **interface-type default, interface-overridable** concern executed **on the node**, not a server-side
  `calc_rule`; 5a builds no readiness-config column, its verdict is the inline probe result.
- **Context:** Reachability history is only honest if the verdict is a **dwell-measurable** signal: availability
  is time-in-state, which needs a categorical state with transitions, not a numeric sample per tick. Modelling
  the verdict as a metric would conflate the raw per-probe reading (`tcp.open`, a firehose sample) with the
  interface-level judgement (an availability substrate), and it would make `time_in_state` a re-derivation over a
  numeric series rather than a read over the state's own transitions. Making it a state, and computing it at the
  node as the AND of the interface's probes, keeps the verdict where the probe results are, keeps the raw metrics
  untouched, and lets the read side reconstruct the availability strip directly from `state_datapoint`.
- **Divergence:** checkpoint 1 seeded the `datapoint_type` canon **metric-only** (the reachability probe metrics),
  and cp3's ingest consumer assumed every surviving datapoint was a metric (`InsertMetricDatapoints` for all).
  This entry records the divergence: 5a adds the first **state** to the seed and makes the ingest consumer route
  by kind (the cp3-deferred "route by kind, not assume metric" note now come due). The `state_datapoint` table
  mirrors `metric_datapoint` (same owner exclusive-arc, same lineage CHECK) with a categorical `value text` plus
  an optional `value_json`.
- **Closes the gap:** the availability SLI (`time_in_state` over `interface.reachable`) and the operator surfaces
  that render the transitions are a later slice (5b); readiness config as an interface-type default is a later
  interface-type concern.
