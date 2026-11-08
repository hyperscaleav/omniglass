---
title: "A node's worklist reaches it in pages, from a bounded responder, retried without exiting"
date: 2026-11-08
status: Proposed
summary: "Proposed: the node pulls its worklist in pages of whole endpoints, each page under the bus's payload limit, from a server responder that serves pulls in parallel up to a bound and stops work on a pull its node has abandoned; a node that misses a pull retries with jittered backoff and never exits on it. The target, a follow-up, is a materialized worklist: the server writes each node's pages to a JetStream key-value bucket when its configuration changes, answers pulls from the bucket, and nudges the node on the reserved worklist-changed subject."
refs: [1577, 1571, 1556, 529]
---

- **Date:** 2026-11-08 | **Status:** Proposed | **Pages:** [nodes](/architecture/nodes/), [messaging](/architecture/messaging/), [scaling](/architecture/scaling/)
- **Decision (proposed, for the architect's choice):**
  1. **Now: (a) a bounded concurrent pull, plus (b) pages.** The worklist pull carries a cursor
     and returns one page of whole endpoints (every task of an endpoint on the same page), each
     page under a byte budget well inside the bus's `max_payload`, and the cursor of the next.
     The server serves pulls from a worker pool bounded by the database pool's size, takes the
     node's remaining budget from the request, and abandons a pull whose budget has run out,
     queued or computing. The node assembles every page before it swaps its worklist, retries a
     failed page with jittered exponential backoff, and never exits on a missed pull; a
     failed reply, on either side, is logged.
  2. **Target: (c) a materialized worklist, served by the server.** A write that changes what a
     node's worklist holds marks the node; a tick materializes each marked node once, in one
     snapshot, into a JetStream key-value bucket keyed by node and page, and publishes on
     `og.v1.worklist-changed.<node>`. A pull is answered from the bucket, with no database
     read, and a pull that names the revision it holds is answered "unchanged". The wire from
     step 1 is kept. Its own issue, after this one.
  3. **Rejected:** a node that watches the bucket itself (it needs JetStream API grants on the
     node credential), raising `max_payload`, and compressing the reply (each moves the ceiling
     and does not remove it).
- **Context:** [#1577](https://github.com/hyperscaleav/omniglass/issues/1577), found by the scale
  lab at 1,000 systems ([#1571](https://github.com/hyperscaleav/omniglass/issues/1571)): the
  headquarters node (806 endpoints, 4,434 tasks) never gets its worklist and crash-loops.
- **The bus today, from the code.** One embedded `nats-server` (v2.15.2) runs in the server
  process with JetStream on; JetStream carries only the telemetry stream (`OG_TELEMETRY`) and
  its durable consumer, and no key-value bucket exists yet (#529). The worklist is **core NATS
  request-reply**. The node calls `nc.Request(og.v1.worklist.<node>, nil, 5s)`
  (`internal/node/run.go`, `worklistTimeout`). The server holds one plain subscription on
  `og.v1.worklist.*` (`internal/bus/server.go`), and nats.go runs one subscription's callbacks
  one at a time on one goroutine, so every node's pull waits behind every pull before it. The
  callback checks the reply inbox, calls `NodeWorklist` with `context.Background()` (so a pull
  the node gave up on is computed in full), marshals the reply to JSON and calls
  `msg.Respond`, discarding its error. `NodeWorklist` (`internal/storage/nodes.go`) is a series
  of separate statements on the pool, not one transaction, so one pull can see two instants.
  The node pulls once at start and **re-pulls the whole worklist every heartbeat tick** (30 s
  by default; the scale lab runs `--heartbeat 2s`). A failed first pull ends `node run`
  (Docker restarts it with growing backoff); a failed tick pull keeps the last worklist and is
  not logged. `config_generation` rides the reply, and the node does not gate on it.
  `og.v1.worklist-changed.<node>` is reserved and already in the node's subscribe grant, and
  nothing publishes on it.
- **What the lab shows (measured 2026-11-08 against a running 1,000-system lab on the worker,
  12 CPUs, main's storage code, without #1571's statement fix):**
  - **The headquarters reply cannot be delivered at any speed.** The server advertises
    `max_payload` 1,048,576 bytes, its default (`server.Options` sets no `MaxPayload`).
    Headquarters' reply marshals to **3,273,096 bytes**, and nats.go refuses to publish a
    message over the limit, so `Respond` fails, the failure is discarded, and the node can only
    time out. A regional node's reply is 1,012,575 bytes before its pins are learned and
    **1,069,588 bytes** after: its first pull fits, and every re-pull after the first ticks is
    dropped the same way, so the six regional nodes silently stop seeing configuration changes.
    A branch is 343,003 bytes. Of headquarters' bytes, task specs are 1,130,222, endpoint
    params 169,554, ladders 265,079, sessions 169,286 and secrets 151,866, each repeated for
    every task of an endpoint. Gzip brings headquarters to 462,967 bytes.
  - **Compute:** `NodeWorklist` took 1.24 s for headquarters and 0.33 to 0.44 s per regional
    with the lab running. All 18 nodes in series took **5.12 s** (17,450 tasks, about 0.29 ms
    a task). In parallel: 2.20 s with a bound of 4, **1.30 s with 8**, and 1.91 s with 18 (the
    database is the limit past about 8). On an idle database with #1571's fix, headquarters
    takes 0.43 s and all 18 in series 1.26 s (#1571's figures).
  - **Load:** with the 17 other nodes pulling every 2 s, the serial responder is offered 5.1 s
    of work every 2 s, 2.5 times what it can serve, so a pull waits behind a backlog even when
    its reply would fit. The server ran at 209% CPU and the database at 361% (ingest included).
- **(a) Concurrent pull, bounded, deadline-honored, retried.**
  - *Wait at a fleet-wide start:* a node waits about the sum of all nodes' compute divided by
    the bound. At 18 nodes, 1.3 s under load (measured), so no node's first pull times out. At
    100 nodes of the same mix (about 97,000 tasks), about 28 s in series and about 7 s with a
    bound of 8 under load (about 1.8 s idle with #1571's fix): the later nodes miss a 5 s pull
    and get it on a retry. The wait still grows with the fleet, more slowly. **On its own it
    does not bring headquarters up:** the reply is still over the payload limit.
  - *Server CPU:* the same total as today, spread over cores. The standing cost stays: every
    node recomputes its whole worklist every tick. At the 30 s default that is about 17% of a
    core at 18 nodes and about 93% at 100, most of it database time, whether or not anything
    changed.
  - *Server restart:* nodes reconnect (unlimited, 2 s apart, jittered) and run their last
    worklist meanwhile; their next pulls arrive together and the bound queues them. *Node
    reconnect:* the node pulls on its next tick, at its own compute cost.
  - *Consistency:* as today: a pull's statements can see a write half-done, and the next pull
    corrects it.
  - *Operations:* nothing new to run.
  - *Migration:* small. A worker pool on the responder, the node's budget in a request header
    (a duration, so clocks need not agree) turned into the context deadline `NodeWorklist`
    already takes, an abandoned pull dropped before it starts, a retry loop with jittered
    backoff around the node's first pull, and the two silent failures logged. No schema
    change.
- **(b) Paged worklists.**
  - *Wait at a fleet-wide start:* each page is a separate, short request with its own
    timeout, so no reply is slow or oversize. Headquarters becomes about 7 pages at a 512 KB
    budget. Total compute is the same as (a) plus a few fixed statements a page (a few
    percent for headquarters). With (a)'s bound, the waits are (a)'s; with no bound, (b) alone keeps
    the serial queue.
  - *Server CPU:* the same as (a), including the per-tick standing cost.
  - *Restart and reconnect:* as (a). A node that loses its connection mid-pull retries the
    page it was on, from its cursor.
  - *Consistency:* each page is read in one snapshot, and an endpoint never splits across
    pages, so every endpoint arrives whole and self-consistent (the gate, the session and the
    fingerprint are all per endpoint). Pages are keyed by endpoint id, so an endpoint added
    behind the cursor mid-pull waits for the next pull, and one moved between nodes can sit on
    neither or both for one tick, which two nodes pulling at different times already allow.
  - *Operations:* nothing new to run.
  - *Migration:* medium, and the only option here that lifts the payload ceiling for good: a
    cursor and a byte budget on the request, the next cursor on the reply, keyset paging in
    `NodeWorklist`, and assembly on the node. No schema change.
- **(c) A materialized worklist, pushed on change.**
  - *Wait at a fleet-wide start:* a pull reads the node's pages from the bucket, a file-store
    read of tens of milliseconds for headquarters' 3.3 MB, and **no database work**, so the
    wait is the same at 18 nodes and at 100. This is the only option where it does not grow
    with the fleet.
  - *Server CPU:* paid when configuration changes, once per marked node (1.24 s for
    headquarters), not on every tick. Pins and rejections land from telemetry and mark their
    node, so marks are coalesced per node per tick. A reconcile pass re-materializes every node
    on a slow interval (for example every 5 minutes: about 2% of a core at 18 nodes and about 9% at 100,
    under load) as the
    backstop for a missed mark.
  - *Server restart:* the bucket lives in the JetStream store, so it survives a restart when
    the store dir is persistent (the lab mounts one). With an ephemeral store, the boot
    re-materializes every node in the background (about 5 s at 18 nodes, about 28 s at 100
    under load), and a node that starts in that window waits for its pages without failing.
    *Node reconnect:* one bucket read, or "unchanged" when it names its current revision.
  - *Consistency:* each node is materialized in one snapshot, better than today. A write
    during materialization marks the node again, so another pass follows. With a second
    server, marks drain as the calc marks do ([ADR-0148](#adr-0148-calc-rules-run-on-a-durable-pending-set-and-a-server-tick-not-a-jetstream-consumer)),
    and the bucket write checks the revision it read.
  - *Operations:* no extra service: the bucket is in the embedded JetStream the server
    already runs, about 14 MB at 1,000 systems with one revision kept. A key-value value is a
    message, so it is held to `max_payload` too, and the bucket stores (b)'s pages.
  - *Delivery:* if the node watched its key itself, its credential would need JetStream API
    grants (a filtered consumer create, stream info, message fetch), widening the per-node
    permission set the confused-deputy guard protects. Instead, the server answers the pull
    from the bucket and nudges on `worklist-changed`, which the node already may subscribe
    to, so the node's grant does not change.
  - *Migration:* large. A schema migration for the marks; a mark written in every write path
    that feeds a worklist (tasks, endpoints and their placement, drivers, secrets down the
    cascade, where a location-level secret marks every node under it, the property lane for
    pins and rejections, and platform settings, about 15 storage files); the materializer
    tick; the bucket; the nudge; the reconcile; and revision handling on the node. Two or
    three PRs, and a missed mark is silent staleness, so the reconcile is not optional.
- **Why this order:** (b) is needed in every design, since the payload ceiling stops
  headquarters before any timing question, and a key-value value has the same ceiling. (a) is
  needed in every design: a node must survive a missed pull, and a responder must not compute
  for a node that has gone. Together they bring the 1,000-system lab up and stop the silent
  regional drops in one PR with no schema change. Only (c) makes a start's wait independent of
  the fleet and removes the per-tick recompute, and its cost is the mark fan-out across every
  writer, which deserves its own slice. Building (c) on (b)'s wire means the node's protocol
  changes once.
- **Open for the architect:** take (a) and (b) now with (c) as the target (recommended); or
  build (c) under this issue as well (two or three PRs); or (a) and (b) only, with no target
  named.
