---
title: "Calc rules run on a durable pending set and a server tick, not a JetStream consumer"
date: 2026-10-21
status: "Accepted, amended (#1018)"
summary: "Component calc rules run on a durable `calc_pending` set marked in the sample-write transaction and drained by a 1s server tick (`SKIP LOCKED`, one transaction per mark), writing calculated samples through the same path so event rules judge them; marks are held by their writer until commit, the ingest retries a deadlock, a failing mark is deferred, a rule write recomputes its owners; a write never marks its own rule; a missing input writes nothing and is recorded in `calc_rule_state`; an unchanged result writes no row. Diverges from the JetStream calc consumer (#430). **Amended (#1018):** every input binds as the list of its per-instance values, an expression's included, and the marking probe reads `calc_rule_input` by key id"
---

- **Date:** 2026-10-21 | **Status:** Accepted | **Pages:** [calculations](/architecture/calculations/), [datapoints](/architecture/properties/), [messaging](/architecture/messaging/)
- **Decision:** Component calc rules run (#996, epic #525 decision 1), as the event rule sweep does
  (ADR-0144 point 6), off the stream:
  1. **Marks in the sample write.** The sample-write transaction inserts a `calc_pending` row (rule,
     owner) for every enabled component rule reading one of the batch's keys on an owner its selector
     picks, found by one probe of the GIN index on `calc_rule.inputs`. A mark already present keeps its
     `marked_at` (coalescing: the oldest unserved change) but is updated rather than skipped, so the
     writer holds it until it commits and a drain cannot serve it from the values before the write (a
     skipped conflict lost the change in review). Marks go in (rule, owner) order. The lock order is the
     event rule rows `FOR SHARE` (the evaluator's load), then the marks, then the series and health locks
     the samples' judging takes.
  2. **A 1s server tick** (`calcTickEvery`, its own goroutine, a 10s deadline per pass, up to 500 marks a
     pass, cancelled on shutdown, a panic recovered and logged, each pass logging what it served and the
     queue's depth) serves due marks oldest first, each in one transaction: the mark `FOR UPDATE SKIP
     LOCKED`, the rule `FOR SHARE SKIP LOCKED` (a rule held by a write has its marks passed over for the
     pass), the mark deleted, the result written. Replicas share the queue and compute each mark once; a
     crash or restart rolls back the mark in flight and loses nothing. A mark whose computation fails is
     recorded in the rule's state and pushed back 30s (`calcRetryBackoff`), and the pass goes on.
  3. **Deadlocks are retried by the ingest.** The tick holds the mark it deleted while its output write
     marks the next rule, and a batch can hold that next mark while waiting on the deleted one. Postgres
     aborts one side (40P01) before anything commits; the ingest retries its whole transaction up to
     `sampleWriteAttempts` times with a jittered pause, and a tick that loses defers its mark.
  4. **Current values.** A named reducer folds each instance's newest value of its input on the
     component: the named instances only, the aggregate instance `''` joining only when the key has no
     other instance, and an instance not heard from within `CalcStaleAfter` (1h) left out and named in
     the state as stale. `majority` breaks a tie by the value that sorts first (`false` before `true`).
     An expression reads the aggregate instance's newest value. Intended values are never read. A missing
     input writes nothing (decision 5); the rule's last outcome per owner, in `calc_rule_state` (computed
     at, rule version, value, missing inputs, stale instances, error), is what the state read (#998) will
     answer from.
  5. **The same write path.** The result is written through the sample-write path as `calculated`, with
     `source_rule` (the rule's name) and `source_rule_version`, on instance `''`, on the lane and type the
     rule holds its output by (never re-resolved by name), so event rules judge it in the tick's
     transaction and downstream calc rules are marked. An unchanged result writes no row on either lane: a
     metric output is change-only too, keeping storage bounded by change.
  6. **A rule write recomputes.** Creating a rule, or any write that bumps its version (enabling one
     included), drops the rule's marks and state rows in the write's transaction and, when it is enabled,
     marks every component its selector picks that holds a value of one of its inputs, in one statement;
     so a rule over stable values computes without a new sample, an edited reduce replaces the old
     output, a disabled rule keeps no state, and a narrowed selector's dropped owners lose theirs.
  7. **No rule feeds itself.** The write carries its producing rule and the marking skips it, which stops
     a self-loop at runtime. Longer loops are refused at authoring under the graph lock (ADR-0147); a loop
     written into the table around the gateway is out of contract, and ADR-0069's provenance rule is
     what stands behind both.
- **Context:** The design puts calc on a trusted JetStream samples lane (#430) that does not exist; the
  event rules already judge in the sample-write transaction. Computing inside that transaction would
  widen ingest's lock footprint with fan-in, and waiting on #430 would block the epic on unbuilt
  infrastructure. A durable pending set plus a tick is replica-safe and coalescing, and keeps a later
  move to the lane a change of transport, not of semantics.
- **Extended 2026-10-21 (#997):** system rules run on the same set. A system's members are its
  role-bound components (the role set the health rollup resolves, one SQL predicate both read); a
  member's sample marks the enabled system rules reading its key on the selected systems it staffs, and
  a role assigned or withdrawn, or a system created, retyped or re-standarded, marks the system's rules
  (before its health recompute, so the mark precedes the health locks). An expression reads
  `members`, one record per member (by name, then id) holding each input under `value.<as>` and its
  presence under `has.<as>`, so one member's inputs stay aligned; a member with no value is named by id in
  the state, and the output is system-owned. A system with no members keeps its last value, its state
  recording `no_members_since`. A retype out of a selector drops the deselected marks first, skipping one
  a serve holds, and a serve rechecks the selection before computing, so neither waits on the other.
  Event rules do not judge system-owned samples yet (#1002).
- **Amended 2026-10-22 (#1018):** point 4's instance and staleness rules now give every input its
  values, expression or not: the reduce is always an expression (ADR-0147's amendment), and each input
  binds as the list of its per-instance values on a component, every member's on a system, where an
  expression read the aggregate instance alone. A system member's record still carries its own
  aggregate value. The marking probe reads `calc_rule_input` by key id (the inputs' GIN index went with
  the jsonb column), so a property and a metric of one name mark only the rules reading that lane's key.
