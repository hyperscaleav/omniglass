---
title: "A node syncs its worklist endpoint by endpoint, against a manifest"
date: 2026-11-14
status: "Accepted"
summary: "Amends ADR-1577's target (item 5): a node no longer pulls its whole worklist in one reply. It pulls a manifest (the ordered config generation, a set digest, and each endpoint's id and content digest), is answered unchanged when it holds that set, and otherwise fetches only the endpoints whose digest moved, in batches capped by bytes. The only reply that grows with the node is the manifest, at about 62 B an endpoint, so the bus's 1 MB limit moves from about 3,000 systems on one site to about 16,800 endpoints on one node. It adds no state on the server, so it does not wait on #1250; a key-value cache, if one comes later, is keyed per endpoint, never per node"
refs: [1586, 1577, 1583, 1584, 1581, 1250, 1872, 1638, 1623, 1945]
---

- **Date:** 2026-11-14 | **Status:** Accepted | **Pages:** [nodes](/architecture/nodes/#pulling-the-worklist), [messaging](/architecture/messaging/), [scaling](/architecture/scaling/)
- **Decision:** (#1586; amends [ADR-1577](#adr-1577-a-nodes-worklist-is-compressed-served-by-a-bounded-responder-and-retried-without-exiting),
  item 5. Recorded before it is built.)
  1. **The node pulls a manifest first.** The manifest carries the node's `config_generation` as
     the reply does today, a **set digest**, and, for each endpoint the node runs, its id and a
     **content digest** (sha256): a digest over everything the node receives for that endpoint (its
     fields, its tasks, and the spec, session and secret entries those tasks name), taking each
     secret by its sealed version, never its plaintext, so a manifest decrypts nothing. The set
     digest covers the sorted ids and content digests and the reply-wide fields (the report window).
     The content digest is not the connection fingerprint
     ([ADR-0158](#adr-0158-one-connection-fingerprint-gates-every-connection-on-a-ladder-the-driver-declares)),
     though it takes secrets the same way: that one gates a connection and ignores the tasks, this
     one says whether the node's copy is current.
  2. **Two values, two jobs.** The pull names both values the node holds. The generation stays the
     ordered value the two-phase move reads ("a generation at or past the clear", on the
     [nodes page](/architecture/nodes/)); a digest has no order, so it cannot confirm a move. The
     set digest is what says unchanged, since a change that moves no endpoint's `updated_at` (a
     rotated secret, an edited spec) still moves it.
  3. **Unchanged is one header.** A set digest that matches is answered `unchanged` with no body,
     and the node keeps everything. This is the steady state, so an idle fleet's pull costs a few
     hundred bytes on the wire and no decrypt. The server still reads and digests the worklist to
     answer it; skipping that read is #1584's input-closure generation.
  4. **Then only what changed, in batches capped by bytes.** The node compares the manifest with what
     it holds and asks by id for the endpoints whose digest differs or that it lacks. The server
     answers whole endpoints, in the order asked, until the batch reaches `max_payload` before
     compression (gzip has held a grouped reply to about a third of that), grouped as
     [ADR-1583](#adr-1583-a-nodes-worklist-travels-grouped-by-endpoint) groups them, each batch
     carrying the dictionary entries its endpoints name; if the compressed batch is still over the
     limit it halves the batch. The node asks again for the rest. A batch is never cut by a count
     of tasks or endpoints, since one https endpoint and one many-task SNMP endpoint differ tenfold
     in bytes.
  5. **An endpoint too large rides alone, and is refused only past the limit.** An endpoint over
     the batch budget is sent in a batch of its own; only an endpoint whose own batch is over
     `max_payload` compressed is refused, by id. The node keeps that endpoint's last copy and every
     other endpoint still syncs, so the failure narrows from the whole site to one device.
  6. **The node applies a set, not a snapshot.** Each served endpoint is self-contained (its fields,
     its tasks, and its own dictionary entries), so the node keeps every copy it is served with the
     digest it was served at, even one that has moved past the manifest it started from. When it
     holds a copy of every endpoint the manifest lists, it applies the set in one step, dropping the
     endpoints the manifest no longer lists, and from then on names that manifest's generation and
     set digest. A fleet that changed mid-sync is caught by the next manifest, never by a restart,
     so a busy fleet cannot keep a sync from finishing. The generation it names is never ahead of
     the endpoints it holds, so a move is never confirmed early. Until a set applies the node runs
     its last worklist, the invariant ADR-1577 set for a failed pull, so a sync cut short never tears
     down a session.
  7. **The wire keeps its subject.** Both requests ride `og.v1.worklist.<node>`, so a node's
     credential gains no grant; one request header says which part is asked for (the manifest, or a
     batch whose body lists the ids). `bus-worklist-pull-count` counts each request, gains an
     `unchanged` outcome, and counts a refused endpoint's batch as `oversize`, now one device's
     request rather than the node's whole pull; `unchanged` is not a loss.
  8. **Any server answers any request.** The manifest and every batch are computed from the
     database on request, so a second server answers a node's next request with no handover.
  9. **A key-value cache, if one comes, is keyed per endpoint.** A materialized cache is an
     optimisation behind the same requests, one entry per endpoint (`<node>.<endpoint>`), each far
     under the limit. It would hold resolved secrets at rest, so it, and only it, waits on
     [#1250](https://github.com/hyperscaleav/omniglass/issues/1250). The change nudge on
     `og.v1.worklist-changed.<node>` stays a separate trigger (#1945: a change that must reach its node in
     under a second).
- **Measured (2026-11-14):** on the 3,000-system corporate soak (read-only) the headquarters node
  runs 1,609 endpoints. A manifest entry is a 36-byte id and a 64-character sha256 digest; a
  synthetic manifest of 1,609 such entries is 196,358 B raw and 100,424 B gzipped (hex digests
  compress poorly), about 62 B an entry, so a manifest reaches the 1 MB limit at about 16,800
  endpoints on one node, ten times headquarters. Past that the manifest is the next ceiling (it
  would page by id range), which this decision does not build. ADR-1583 measured headquarters'
  grouped reply on its 3,000-system seed at 1,508,216 B raw and 463,378 B gzipped, so a cold start
  there is the manifest and two or three batches (each repeats the dictionary entries it names).
- **Divergence from ADR-1577:** item 5 named the materialized worklist as the target, gated on
  #1250: the server writes (b)'s pages into a per-node bucket when the worklist changes, so a pull
  does no database work and its wait does not grow with the fleet. That lifts the 1 MB ceiling too,
  but it needs every writer that can change a node's worklist to mark it, a reconcile for a missed
  mark, and resolved secrets at rest. This decision lifts the ceiling with no new state on the
  server, and the bucket becomes the optional per-endpoint cache of item 9. The price is the one
  ADR-1577's target avoided: until #1584 a steady-state pull still reads and digests on the server,
  though it sends and decrypts nothing.
- **Consequences:** a pull becomes two or more requests on a change (the manifest, then batches),
  and one on no change. The node changes how it holds its worklist: today it flattens the grouped
  reply into one id-sorted task list and keeps only that; it will keep each endpoint's group as
  served, keyed by endpoint id with its digest, and derive the task list when a set applies. The
  grouping becomes exactly one group per endpoint id (today one endpoint can split into several
  groups). The node and the server change their pull together, and with no deployment outside dev
  and the lab no compatibility path is kept for the one-reply pull. #1586 is rewritten to this
  shape; #1584 makes item 3 skip the read; #1581 makes the batch reads cheap.
- **Not chosen:**
  - **The per-node bucket (ADR-1577, item 5), as the first step.** It lifts the ceiling and takes
    the read off the pull, but only with marks in every writer, a reconcile and secrets at rest
    (#1250); the sync needs none of them, and a per-endpoint cache can still sit behind it.
  - **Paging the one reply.** A cursor over the reply lifts the cap, but every pull still sends the
    whole worklist, page by page, changed or not, and the node assembles pages that may straddle a
    change.
  - **A pull per component or per endpoint.** No reply grows, but a cold headquarters start is
    1,609 requests, each a database read, where the sync needs three or four.
  - **Raising `max_payload`.** Rejected in ADR-1577 and still: it moves the ceiling.
