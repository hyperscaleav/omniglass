---
title: "A node syncs its worklist endpoint by endpoint, against a manifest"
date: 2026-11-14
status: "Accepted"
summary: "Amends ADR-1577's target (item 5): a node no longer pulls its whole worklist in one reply. It pulls a manifest (a version, and each endpoint's id and content digest), is answered unchanged when it holds that version, and otherwise fetches only the endpoints whose digest moved, in batches capped by bytes, and swaps them in whole. No reply grows with the node, so the bus's 1 MB limit stops being a ceiling on a site. It needs no secrets at rest, so it does not wait on #1250; a key-value store, if one comes later, is keyed per endpoint, never per node"
refs: [1586, 1577, 1583, 1584, 1581, 1250, 1872, 1638, 1623]
---

- **Date:** 2026-11-14 | **Status:** Accepted | **Pages:** [nodes](/architecture/nodes/#pulling-the-worklist), [scaling](/architecture/scaling/)
- **Decision:** (#1586; amends [ADR-1577](#adr-1577-a-nodes-worklist-is-compressed-served-by-a-bounded-responder-and-retried-without-exiting),
  item 5. Recorded before it is built.)
  1. **The node pulls a manifest first.** The manifest is a version and, for each endpoint the node
     runs, its id and a **content digest**: a digest over everything the node receives for that
     endpoint (its fields, its tasks, and the spec, session and secret entries those tasks name).
     The version is a digest over the sorted ids and digests and the reply-wide fields (the report
     window). The content digest is not the connection fingerprint
     ([ADR-0158](#adr-0158-one-connection-fingerprint-gates-every-connection-on-a-ladder-the-driver-declares)):
     that one gates a connection and ignores the tasks, this one says whether the node's copy is
     current.
  2. **Unchanged is one header.** The pull names the version the node holds; a match is answered
     with no body, and the node keeps everything. This is the steady state, so an idle fleet's pull
     costs a few hundred bytes on the wire, not the whole worklist. Answering it without building the
     worklist first is #1584's input-closure generation; until that lands the server builds, digests
     and compares, which saves the wire and the node's work but not the server's read.
  3. **Then only what changed, in batches capped by bytes.** The node compares the manifest with
     what it holds and asks for the endpoints whose digest differs or that it lacks, by id. The
     server answers with as many of them as fit a byte budget (half of `max_payload` before
     compression, so a batch fits with or without gzip), grouped as
     [ADR-1583](#adr-1583-a-nodes-worklist-travels-grouped-by-endpoint) groups them, each batch
     carrying its own dictionaries; the node asks again for the rest. A batch is never cut by a
     count of tasks or endpoints, since one https endpoint and one many-task SNMP endpoint differ
     tenfold in bytes.
  4. **The node swaps whole or not at all.** Every endpoint in a batch carries its digest. When all
     the changed endpoints arrive at the manifest's digests, the node applies the new set in one
     step, dropping the endpoints the manifest no longer lists. A digest that disagrees with the
     manifest (the fleet changed mid-sync) abandons the sync and the node starts over from a fresh
     manifest on a short backoff; until a sync completes it runs its last worklist. A node never
     runs a partial worklist, the invariant ADR-1577 set for a failed pull, so a sync cut short
     never tears down a session.
  5. **One endpoint too large is refused by name.** An endpoint whose batch alone is over
     `max_payload` compressed is refused with its id, the node keeps that endpoint's last copy
     and counts the refusal in `worklist-pull-loss-ratio`, and every other endpoint still syncs.
     The failure narrows from the whole site to one device.
  6. **Any server answers any request.** The manifest and every batch are computed from the
     database on request, so a second server answers a node's next request with no handover. This
     is what ADR-1577's per-node bucket was for.
  7. **A key-value store, if one comes, is keyed per endpoint.** A materialized cache is an
     optimisation behind the same requests, one entry per endpoint (`<node>.<endpoint>`), each far
     under the limit. It would hold resolved secrets at rest, so it, and only it, waits on
     [#1250](https://github.com/hyperscaleav/omniglass/issues/1250). The change nudge on
     `og.v1.worklist-changed.<node>` stays a separate trigger (a change that must reach its node in
     under a second).
- **Measured (2026-11-14, the 3,000-system corporate soak, read-only):** the headquarters node runs
  1,609 endpoints; its grouped reply (ADR-1583) is 1,508,216 B raw and 463,378 B gzipped, about
  940 B and 290 B an endpoint. A manifest entry is a 36-byte id and a digest, about 70 B raw and
  40 B gzipped (an estimate from the field sizes, not a measurement), so headquarters' manifest is
  about 64 KB gzipped, and a manifest reaches the 1 MB limit at about 26,000 endpoints on one node.
  A cold start at headquarters is the manifest and three batches.
- **Divergence from ADR-1577:** item 5 named the materialized worklist, one bucket entry per node,
  as the target, gated on #1250. A key-value value is a message, so a per-node entry carries the
  same 1 MB cap as the reply it replaces: it moves the database read off the pull path but does not
  lift the ceiling #1872 hit. This decision lifts the ceiling first and leaves the cache to be added
  per endpoint when a trigger holds.
- **Consequences:** a pull becomes two or more requests on a change (manifest, then batches), and
  one on no change. The node keeps its worklist keyed by endpoint with each digest, which it already
  nearly does (the reply is grouped by endpoint). The node and the server change their pull together,
  and with no deployment outside dev and the lab no compatibility path is kept for the one-reply
  pull. #1586 is rewritten to this shape; #1584 makes step 2 skip the build; #1581 makes the batch
  reads cheap.
- **Not chosen:**
  - **The per-node bucket (ADR-1577, item 5).** Same 1 MB cap per entry, and secrets at rest, so it
    waited on #1250 without fixing the limit.
  - **Paging the one reply.** A cursor over the reply lifts the cap, but every pull still sends the
    whole worklist, page by page, changed or not, and the node assembles pages that may straddle a
    change.
  - **A pull per component or per endpoint.** No reply grows, but a cold headquarters start is
    1,609 requests, each a database read, where the manifest needs four.
  - **Raising `max_payload`.** Rejected in ADR-1577 and still: it moves the ceiling.
