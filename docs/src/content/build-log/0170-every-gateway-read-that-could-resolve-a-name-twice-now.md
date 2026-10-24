---
title: "Every gateway read that could resolve a name twice now resolves it once and binds the uuid"
date: 2026-08-08
refs: []
form: bullet
---

- **Every gateway read that could resolve a name twice now resolves it once and binds the uuid**
  ([#627](https://github.com/hyperscaleav/omniglass/issues/627) Task 10, ahead of the placement-scoped
  uniqueness DDL below). A first pass (`840066a`) converted roughly 40 inline scalar name subqueries
  gateway-wide to resolve a reference once via `scopedByName` and bind the resulting uuid. A controller
  trace rejected that pass's own deferral of the collection ingest write path: `current_values.go`,
  `settlement.go`, and `property_samples.go` still resolved a component, system, or location by an
  inline name subquery on the hot path, which the moment two rows shared a name would raise SQLSTATE
  21000 or silently fail a CHECK constraint; the same fix round also found `EffectiveProperties` and
  `EffectiveMetrics` resolving their owner inside a CTE rather than a scalar subquery, which never
  raises 21000 at all, it silently joins rows from every same-named owner into one answer, and a
  `CreateSystem`/`UpdateSystem` recompute passing a system's own freshly-written name, with its id
  already in hand, into the health rollup (`d64b314`). A 30-agent adversarial sweep then found six
  more of the same class the first two passes had missed: the node ingest lane discarding a task's own
  component uuid for a re-derived name at four downstream sinks (`cab918e`), the push telemetry route
  authorizing by id then publishing `comp.Name` (`dd92017`), five role-choice writes binding a raw name
  into a scalar subquery (`47864bc`), a standard-owned role recompute re-deriving conforming systems'
  ids from names and raising an unmapped `ErrAmbiguousName` that rolled back the whole write rather
  than failing usefully (`4707bcb`), the command and reconciliation routes re-deriving an id from a
  name the gateway had just resolved moments earlier (`63b715e`), and effective tags' own `?system=`
  filter repeating the CTE silent-union shape in its `seed_sys` clause (`7802b32`). A later,
  independent hunt, told to ignore all prior analysis, wrote a scanner over every SQL string literal in
  the tree and verified closure rather than finding anything new: only four production sites still
  resolve by bare-name scalar subquery, all four verified safe (the three `*NameTaken` advisories, and
  `roleOwnerExpr`'s standard branch, whose caller pre-resolves the id). No caller-observable behaviour
  changed throughout: every existing fixture used globally-distinct names, so the whole suite stayed
  green, and the new duplicate-name tests (`b84a398`, `d889bbc`, `839d4d1`) are the only regression net
  this class of bug now has. Filed [#641](https://github.com/hyperscaleav/omniglass/issues/641):
  effective tags' `?system=` filter still resolves outside the caller's own read scope, narrowed by the
  uuid bind but not closed, deliberately left for a later slice since this task's own contract was to
  change no observable behaviour.
