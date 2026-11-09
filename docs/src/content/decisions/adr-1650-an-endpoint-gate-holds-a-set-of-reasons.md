---
title: "An endpoint gate holds a set of reasons"
date: 2026-11-09
status: Accepted
summary: "Accepted: ADR-0158's one refusal per endpoint becomes a set of reasons, each kind with its own rank, scope and release rule; refused is one kind and unreachable, from two consecutive down probe verdicts, is the second. Polls under unreachable are held, counted as poll-held-count, never as failures or loss, with no backoff. Disrupted, upstream and cross-node reasons build on the set (#1651, #1652, #1655)."
refs: [1650, 1624, 1651, 1652, 1655, 1249, 1657]
---

- **Date:** 2026-11-09 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/#the-connection-fingerprint-and-the-layered-gate), [nodes](/architecture/nodes/#implicit-reachability)
- **Decision:**
  1. **The gate is a set of reasons.** [ADR-0158](/architecture/decisions/#adr-0158-one-connection-fingerprint-gates-every-connection-on-a-ladder-the-driver-declares)
     held one refusal per endpoint. The node now holds a set, keyed by kind, and one table names
     each kind's rank, scope and release rule: `refused` (rung-scoped, released when the
     connection fingerprint changes or the rung passes) and `unreachable` (the whole host,
     released on the probe's next up verdict). The gate is closed while any reason holds, removing
     one kind leaves the others, and `closed` names the most causal reason, ties to the oldest.
  2. **Refusal-only questions see only refusals.** The rung checks, the ssh probe's credential, a
     held session's start and a datagram listener's re-arm ask `refusedAt`, which ignores every
     other kind, so an unreachable endpoint keeps its probe credential and starts unrejected when
     the device returns. Polls, commands, run now and previews ask the whole set.
  3. **Two consecutive downs close it.** The nodes page said poller tasks run only if the verdict
     is up; one lost probe would then stop collection for an interval. `unreachable` is added on the
     second consecutive down verdict, dated from the first; an inconclusive probe neither adds nor
     resets the streak; the next up removes it.
  4. **Held polls are not loss.** A due poll under `unreachable` is held: nothing is sent, no
     failure and no backoff are booked, and the window counts it as `poll-held-count`, apart from
     `poll-failure-count`. The node chose not to send it, so it is not a lost sample, and a loss
     ratio built on failures stays honest. The first tick after the up verdict runs it. An endpoint
     with no probe has no verdict and fails and backs off as before.
  5. **A held endpoint answers at once.** A command answers `unreachable since <time>` without
     dialing; a run now and a preview answer 409 naming the reason, not 502.
  6. **One published value, refusal first.** `endpoint-rejection` renders the refusal as before,
     then each other reason after a `;` as `<kind> <since> <source>`. `rejection.Parse` still
     returns only the refusal, so every existing reader is unchanged; `rejection.ParseSet` reads the
     whole set and the reachability read serves it as `gate_reasons`. A restarted node adopts the
     refusal only: an unreachable reason is the probe's to re-earn.
- **Divergences and follow-ups:** held sessions and listen tasks are not gated by `unreachable`
  (their own reconnect handles a gone device). The `disrupted` kind is #1651, upstream reasons
  #1652, and a reason seen by another node #1655. The console rendering the set is #1657.
- **Rejected:** a second gate map beside the refusal (two sources of truth for one question, and
  every caller would have to ask both); closing on the first down verdict (one dropped probe stops
  an interval of collection); counting held polls as failures (a gone device would read as loss
  from the node, not from the device, and back off its return).
