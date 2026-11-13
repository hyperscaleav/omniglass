---
title: "A node's worklist travels grouped by endpoint"
date: 2026-11-13
status: "Accepted"
summary: "Amends ADR-1577's deferred normalized wire: a worklist reply carries each endpoint's fields once with its tasks under it, and the task specs, session blocks and credentials its tasks share once in dictionaries named by index. The server fingerprints each endpoint once. Headquarters at 3,000 systems is 463 KB gzipped with every pin learned, under the 512 KB line, so paging is not built"
refs: [1583, 1577, 1872, 1623]
---

- **Date:** 2026-11-13 | **Status:** Accepted | **Pages:** [nodes](/architecture/nodes/#pulling-the-worklist)
- **Decision:** (#1583; amends [ADR-1577](#adr-1577-a-nodes-worklist-is-compressed-served-by-a-bounded-responder-and-retried-without-exiting), item 4.)
  1. **The reply is grouped by endpoint.** Its `endpoints` list carries each endpoint's fields (its
     name, transport, params, ladder, learned pin, last rejection, fingerprint, component and path
     upstreams) once, and under each the tasks that run over it: an id, a mode, and an index into
     each of three dictionaries the reply carries once, `specs`, `sessions` and `secrets`. Index 0
     is none (a probe has no session block). A task naming an entry the reply does not carry fails
     the whole pull, never one task. Tasks of one endpoint that disagree on an endpoint field get a
     group each, so the wire never drops a difference.
  2. **The node reads the same tasks.** The grouping is the JSON form of `collection.WorklistReply`
     itself, so the server and the node share one Go type and the node's dispatcher, session
     reconcile and gates see the flat task list they did before, in id order.
  3. **The server fingerprints an endpoint once.** Every input of a fingerprint is the endpoint's,
     so `NodeWorklist` computes one per endpoint, not one per task.
  4. **No paging.** ADR-1577 left paging conditional on headquarters at 3,000 systems still being
     over 512 KB gzipped; grouped, it is under.
- **Measured (2026-11-13, the corporate plan seeded on a fresh database, every https endpoint's pin
  learned, as on the soak):**

  | Headquarters node | Tasks | Flat raw | Flat gzipped | Grouped raw | Grouped gzipped |
  |---|---|---|---|---|---|
  | 1,000 systems | 4,436 | 3,446,419 B | 575,058 B | 761,222 B | 233,385 B |
  | 3,000 systems | 8,850 | 6,875,172 B | 1,172,107 B | 1,508,216 B | 463,378 B |

  The flat 3,000 figure matches the soak's headquarters reply to the byte (#1872), which was over
  the bus's 1 MB limit.
- **Divergence from the ticket:** #1583 asked for the wire's schema to be generated. There is no
  generator for collection types, and the wire has one producer and one consumer compiled from the
  same type, so the Go type is the single source; a generated schema would describe a contract no
  third party reads.
- **Consequences:** a node and a server of different builds before this change cannot read each
  other's worklist. With no deployment outside dev and the lab, no compatibility path is kept. The
  learned pin and the last rejection now cost their bytes once per endpoint rather than once per
  task, so a node learning its pins grows its reply by about 31 KB at headquarters, not 343 KB.
- **Not chosen:**
  - **Paging.** It lifts the ceiling for good, at the cost of a cursor, a byte budget and node-side
    assembly; at 463 KB against 1 MB it buys nothing yet, and stays the move if a larger site
    crosses 512 KB.
  - **A binary encoding.** Gzip over the grouped JSON already removes most of the repetition, and a
    new codec is a dependency on both ends for the rest.
  - **Grouping only, no dictionaries.** The task specs, session blocks and credentials of one driver
    repeat across every endpoint it runs on; they were the largest share of the flat reply.
