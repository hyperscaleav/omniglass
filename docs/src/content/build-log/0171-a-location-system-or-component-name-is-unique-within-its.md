---
title: "A location, system, or component name is unique within its placement, not the whole estate"
date: 2026-08-08
refs: []
form: bullet
---

- **A location, system, or component name is unique within its placement, not the whole estate**
  ([#627](https://github.com/hyperscaleav/omniglass/issues/627) Task 11, `f132a80`, `87207b7`,
  [ADR-0089](/architecture/decisions/#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup)).
  Each of the three tables trades its global `UNIQUE (name)` constraint for a set of partial unique
  indexes, one per placement bucket, plus a plain btree for the ambiguity scan a bare-name resolve now
  has to run. `component` and `system` each carry three buckets (parented, located-but-unparented,
  orphan/root); `location`, which carries no `location_id` column of its own, carries two (parented,
  root). A bare name matching more than one row in the caller's scope is a `409` naming the candidates. A review round (fix round 1: `f55daa2`,
  `829dc6e`, `9e3c017`, `0e4aea0`, `c097dd6`, the last also rekeying the console's tree builders
  (Components, Systems, Locations, the placement pickers) off the now-ambiguous name onto uuid) closed
  its own findings but introduced two new criticals in doing so: a cross-tier scope check that denied
  every non-all-scoped caller on a write carrying a location or system binding, and a scoped
  `component:read` caller's effective-tags system band silently vanishing, neither caught by a fresh
  full test run because no existing test covered a scoped non-all principal on a write path carrying a
  cross-tier binding. The next round fixed both by a byte-identical revert to the pre-task baseline
  rather than a new authorization posture, with two tests genuinely red against the introducing round
  closing the coverage hole that had let them ship (`ba8a9e6`). The same round converged three
  near-identical scope-resolution functions (`scopedByName` / `scopedByNameInScope` /
  `resolveScopedRef`) into one, `resolveRef`, with a policy axis for the read-versus-write disclosure
  split, on the ruling that the converged primitive, checking its caller-supplied scope against the
  resource it was actually resolved for, would have caught both criticals at the first scoped test
  rather than after (`1cf8d74`, `9de2fe7`). A forward-insurance tier guard landed on the same
  primitive, documented as unable to fire on any input the current code produces (`48db30f`). Six
  known edges this convergence ships with, stated rather than hidden, are recorded in
  [ADR-0089](/architecture/decisions/#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup).
