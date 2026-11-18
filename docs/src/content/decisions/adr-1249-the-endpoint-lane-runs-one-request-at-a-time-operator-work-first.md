---
title: "The endpoint lane runs one request at a time, operator work first"
date: 2026-11-09
status: "Accepted"
summary: "Every discrete request a node makes rides its endpoint's lane on the bounded worker pool (#1249): scheduled polls and probes, and an operator's commands, run nows and previews, one in flight per endpoint, endpoints side by side. Operator work waits in a class ahead of the scheduled work, never preempting the request in flight. Each job consults its endpoint's gate as it is taken, so work queued behind a reboot or a refusal answers with the gate's reason and sends nothing, the most causal reason first. A preview resolves to the inventoried endpoint its transport and target name and takes that lane, gated by that endpoint's gate, a refusal included; one that names none runs off-lane, held only by its host. Endpoints on one host share one ping per probe slot. Operator work waiting across the lanes is capped at 64, a refusal answering at once (the API's 429) and counted against the offers. `--lane-workers` replaces `--poll-workers`. Amends ADR-1634 and ADR-1163"
refs: [1249, 1634, 1163, 1649, 1650, 1651, 1254, 1063, 1625]
---

- **Date:** 2026-11-09 | **Status:** Accepted | **Pages:** [nodes](/architecture/nodes/#the-endpoint-lane)
- **Decision:** (#1249, on the lane pool of [ADR-1634](/architecture/decisions/#adr-1634-each-poll-and-probe-runs-on-its-own-interval-from-a-dispatcher-of-its-own).)
  1. **The lane is the endpoint.** A lane is keyed on the endpoint's id: one discrete request in
     flight per endpoint, two endpoints of one component running side by side, so a slow device
     holds only its own lane. Scheduled polls and probes, commands, run nows and previews that
     resolve to an endpoint all take it. Listen tasks never do: they hold the endpoint's session or
     connection and are standing receivers, not requests.
  2. **Operator work first, nothing preempted.** A lane has two classes: an operator's commands, run
     nows and previews wait ahead of the scheduled work queued on the same lane. The request in
     flight always finishes.
  3. **The gate is consulted as a job is taken.** A lane holds no state about its endpoint. A job
     taken off it asks the endpoint's gate first (and a run now or preview asks again as it arrives,
     so a gate already closed answers without waiting out the lane). A queued command fails naming
     the reason and sends nothing, a run now answers `Gated`, a scheduled poll is held and counted.
     Where several reasons hold, the most causal answers: `disrupted`, then `refused`, then
     `unreachable`, the oldest within a kind. A refusal holds only a job that sends its own request
     at or above the refused rung, since a job riding a held session is answered by that session.
  4. **A preview resolves to its endpoint.** The node indexes its worklist by transport and target
     (the lowest endpoint id where two name the same). A preview whose polls all name one
     inventoried endpoint takes that endpoint's lane and is gated by that endpoint's gate, a refusal
     included, since a preview sends its own request. A preview that names none, or names several,
     runs off-lane, held only by a host-wide reason on any endpoint at its target's host, whatever
     transport it speaks.
  5. **One ping per host per probe slot.** Every endpoint whose probe falls due in the same slot
     (the clock truncated to the probe interval) on one host rides one ping: the first sends it, the
     rest read its answer. Each endpoint's probe still reads its own transport and session rungs.
  6. **Overload is counted, never queued without bound.** Scheduled work already skips a due whose
     run is in flight (ADR-1634). Operator work waiting across every lane is capped at 64: a run now
     or preview past the cap answers at once that the lanes are full (`RunReply.Full`, the API's
     429), and the dispatch window counts `operator-offer-count` against `operator-refused-count`,
     saying both in its `dispatch window` self-log line for a window that refused one. Commands do
     not take a cap slot: the node actuates its pulled commands one at a time, each waiting for its
     endpoint's lane, so they are bounded by that loop.
  7. **The flag names the pool.** `--lane-workers` (`OMNIGLASS_LANE_WORKERS`, default 16) replaces
     `--poll-workers`: the pool runs every kind of lane work, not polls alone.
- **Amended by [ADR-1869](#adr-1869-a-node-sizes-its-lane-pool-from-its-cpu-and-memory):** the
  `--lane-workers` default is `auto`, sized from the CPU and memory the node may use, not 16.
- **Amends:** [ADR-1634](/architecture/decisions/#adr-1634-each-poll-and-probe-runs-on-its-own-interval-from-a-dispatcher-of-its-own)
  (the `--poll-workers` flag, and a run now handed through the run loop and refused while the last
  one waited there) and
  [ADR-1163](/architecture/decisions/#adr-1163-a-poll-runs-once-on-demand-through-its-node-as-a-preview-that-stores-nothing-or-a-run-now-that-lands)
  (a preview on the request's own goroutine beside any endpoint's work, held only by its host).
- **Context:** Before this, the lane pool already ran scheduled work and commands per endpoint, but a
  preview of a device already attached ran beside that device's polls and ignored its refusal, a run
  now waited in a one-slot handoff that refused a second caller, a closed gate answered whichever
  reason its caller checked first rather than the one that caused the rest, and three endpoints on
  one host each sent their own ping. A device that takes one request at a time is the
  common case in AV gear; the lane makes that the default and the gate makes queued work honest
  about why it did not run.
- **Not chosen:**
  - **A second scheduler for operator work.** Two pools would race for the same device; one pool
    with a class per lane keeps one request in flight per endpoint by construction.
  - **Preempting the request in flight.** A half-sent request to a device leaves it in an unknown
    state; the operator waits at most one request.
  - **A concurrent reachability pre-pass before each poll.** It doubles the traffic and still races
    the poll; the probe on its own interval feeds the gate, and the gate is read at take.
  - **Snapshotting the gate when work is queued.** A reboot ahead of it on the lane closes the gate
    after the job was queued; only a read at take sees it.
  - **A component-wide lane by default.** Most components' endpoints are independent; a driver opts
    a fragile device into component-wide exclusion (#1649). More than one request in flight to one
    endpoint is a held session's pool size (#1254).
  - **An unbounded operator queue.** It hides overload until memory runs out; a counted refusal
    tells the caller to retry and shows the pressure.
