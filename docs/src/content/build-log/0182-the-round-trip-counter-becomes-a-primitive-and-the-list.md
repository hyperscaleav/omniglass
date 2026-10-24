---
title: "The round-trip counter becomes a primitive, and the list paths are pinned"
date: 2026-08-09
refs: []
form: bullet
---

- **The round-trip counter becomes a primitive, and the list paths are pinned**
  ([#650](https://github.com/hyperscaleav/omniglass/issues/650), out of
  [#643](https://github.com/hyperscaleav/omniglass/issues/643)). The batch address walk shipped with a
  counting querier inlined in its own test file, which made that one file, accidentally, the repo's
  entire performance regression net: no benchmarks, no bench target, no perf workflow. It is now
  `internal/storage/storagetest/querycount`, with tests of its own, and the test that introduced it
  consumes it rather than keeping a copy.

  Promoting it forced the design question the inline version never had to answer. The old wrapper
  wraps the internal two-method querier, which observes the address-attach hooks because they take
  their querier as a parameter, and observes **nothing** on a gateway `List`, which takes a `scope.Set`
  and queries the pool directly. A wrapper handed to one of those would have reported a small, flat,
  entirely fictional number, and every assertion built on it would have passed while measuring nothing.
  So the seam is the pool: `storage.WithQueryTracer` installs a `pgx.QueryTracer` on the connection
  config (legitimate production observability, where an OpenTelemetry tracer attaches, not only a test
  affordance), `NewPG` parses the DSN into a config to reach it, and `storagetest.NewCountingDB` hands
  a test a gateway whose every statement is counted. The primitive's doc comment leads with that
  hazard, because a count means nothing if the code under test does not go through the counted seam.

  Nine list paths now carry an assertion, each checking both flatness across page sizes and an
  absolute ceiling, since equality alone passes a read that is still per-row with a smaller constant
  and a ceiling alone passes one that is flat at a bad number: components, systems and locations at
  four, four and two statements; members at five; alarms at two; interfaces, tags and roles at one.
  Every one of them was proven by mutation, breaking the batch walk, the join and the per-row read in
  turn and watching the specific number move. The fixtures spread their rows across several rooms and
  several depths on purpose: an address walk grows with the distinct rooms a page spans, so a page
  confined to one room makes a per-room loop look exactly as flat as a batch.

  Pointing the instrument at the paths the issue did not name turned up a real one. `ListPrincipals`
  drains its base query and then calls `loadPrincipal` per row, three more statements each (kind
  profile, effective grants, group memberships), so the admin directory reads at 1+3N: four statements
  for one principal, sixty-one for twenty. It is pinned at its current shape rather than fixed here,
  under a test whose name says it pins a defect and whose failure message says the fix is to delete it.
  (Fixed and the pin deleted in [#671](https://github.com/hyperscaleav/omniglass/issues/671), below.)
