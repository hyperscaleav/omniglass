---
title: "A worklist change nudges its node through a Postgres notify on commit"
date: 2026-11-15
status: Accepted
summary: "A write that changes a node's worklist issues a Postgres NOTIFY naming the node inside its own transaction; one listening connection on the server forwards each name as an empty message on og.v1.worklist-changed.<node>, and the node pulls at once, at most once every 200 ms. The notification is not durable and not replayed: the scheduled pull stays the backstop. Diverges from #1945's spec, which named the outbox."
refs: [1945, 1997, 1586, 1638, 1577, 81]
---

- **Date:** 2026-11-15 | **Status:** Accepted | **Pages:** [nodes](/architecture/nodes/), [messaging](/architecture/messaging/)
- **Decision:** (#1945, building the nudge [ADR-0081](#adr-0081-the-control-plane-wire-is-one-subject-grammar-node-anchored-and-batch-granular)
  reserved and [ADR-1586](#adr-1586-a-node-syncs-its-worklist-endpoint-by-endpoint-against-a-manifest) item 9 kept separate.)
  1. **The trigger is met.** A node pulled only on its `--worklist-every` interval, 30 s jittered
     within 10%, so an operator edit waited up to about 33 s to run, and a move between nodes
     ([ADR-1638](#adr-1638-a-sites-components-spread-across-its-nodes-by-expected-cost) item 7)
     waited a pull on each side. The target is under a second.
  2. **The write announces inside its transaction.** The Storage Gateway, in the transaction of a
     write that changes a node's worklist, runs `pg_notify` on one channel with each touched node's
     name as the payload. Postgres delivers a notification only on commit and folds identical
     payloads within a transaction, so a rolled-back write announces nothing and a write touching
     a node's endpoints many times announces it once. The notify is issued by the Go write path,
     not a database trigger. This slice covers the endpoint writes (create, edit, session retry,
     delete) and a driver spec change that re-derives an endpoint's tasks; the other writers are
     #1997.
  3. **One listener, outside the pool.** The bus holds one dedicated connection that `LISTEN`s on
     the channel and publishes each name as an empty message on `og.v1.worklist-changed.<node>`. The
     connection is re-opened on a backoff (100 ms doubling to a 5 s cap) and the drop logged at
     WARN. A notification sent while it is down is lost, not replayed.
  4. **The node coalesces and rate-limits.** It subscribes before its first pull, holds at most one
     pending nudge, pulls at once on one, and pulls at most once every 200 ms. During a failed
     pull's backoff it ignores nudges, so a nudge never turns the backoff into a retry storm.
  5. **The scheduled pull stays.** The nudge carries no content and is not durable; the interval
     pull is still what bounds staleness when a nudge is lost.
- **Context:** #1945's spec said the commit publishes "through the outbox". The outbox is the
  `notification` table, delivered by a worker that claims due rows on a poll: each change would
  write a row, and its delivery would wait on that poll, which is the latency the nudge exists to
  remove. Its value is durability and retry, which a nudge does not need: losing one costs only
  the wait for the next scheduled pull. A `NOTIFY` has the outbox's one property that matters here,
  that it leaves the database only if the change commits, with neither its row nor its poll.
- **Consequences:**
  - In the end-to-end test, an edit runs on its node within a second with the scheduled pull set to an hour,
    and a burst of 100 edits costs 3 pulls over 0.8 s (`TestAWorklistChangeReachesTheNodeAtOnce`).
  - An edit that changes nothing a node runs (a label) still nudges, and costs that node one pull.
    The manifest sync (#1586) makes such a pull answer `unchanged`.
- **Not chosen:**
  - **The outbox:** a row and a poll per change for a message that may be lost (Context).
  - **A database trigger on `endpoint`:** it misses the writes that change a worklist through
    another table (a driver spec, #1997's writers) and puts behavior in the schema the Storage
    Gateway cannot see. The Go call sits beside the audit write of the same transaction.
  - **Publish from the API handler after commit:** every handler would have to remember it, outside
    the Gateway, and a crash between the commit and the publish loses the nudge just as a lost
    `NOTIFY` does, so it buys nothing for the spread.
