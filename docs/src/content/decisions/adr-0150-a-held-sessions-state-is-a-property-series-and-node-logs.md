---
title: "A held session's state is a property series and node logs, not a session table"
date: 2026-10-23
status: "Accepted"
summary: "A held session (#816) surfaces as the `session-state` property per endpoint (establishing, up, recovering, down; transitions only, under the endpoint's standing listen task) plus the node's self-logs carrying the reasons; no `session_log` table and no `session` view. The session is held for an endpoint whose worklist carries a standing listen task over a transport with a session dialer (the line protocol over `tcp` today), polls on that endpoint borrow it, and recovery is a jittered exponential backoff with an attempt budget before down"
---

- **Date:** 2026-10-23 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [nodes](/architecture/nodes/), [datapoints](/architecture/properties/), [glossary](/architecture/glossary/), [storage](/architecture/storage/)
- **Decision:** A stateful transport holds its connection (#816, epic #603's thin cut "no session
  table"), and its lifecycle is surfaced without a new store:
  1. **The state is a property.** `session-state` (seeded, `establishing`, `up`, `recovering`,
     `down`) lands per endpoint on the endpoint's component (instance: the endpoint's name, as
     `endpoint-reachable` does), published by the node at the moment of each transition under the
     endpoint's standing listen task, so ownership binds server-side like every other sample and the
     series is transitions only.
  2. **The reasons are node logs.** Each transition is also a self-log (`facility: session`,
     ADR-0066) carrying the reason (`connection lost: EOF`, `5 consecutive attempts failed, last:
     ...`), and teardown logs why (`its listen task left the worklist`, `its endpoint changed`, `node
     shutdown`). The reason also rides the sample as the `reason` label, which is not persisted.
     Teardown publishes no state: the node stopped wanting the session, which says nothing about the
     device. So the last `session-state` outlives a teardown or a node shutdown (an `up` stays the
     latest value of a session nobody holds), and a read must judge it by its age, as the
     property catalog already says of stale values. No surface reads `session-state` yet (the
     reachability panel reads the endpoint verdicts only); the console work that shows it, with
     #817's listeners, must render it aged. **Amended (#1059):** the console reads it now, on the
     component's reachability read beside `session-reason` and the gate's rung and class, aged by the
     endpoint's `last_seen` (a state whose endpoint delivered nothing for the staleness window reads
     grey), with Retry for a rejected one.
  3. **What holds a session.** An endpoint whose worklist carries a standing listen task over a
     transport with a session dialer (the line protocol over `tcp` today; ssh is not wired). The
     listen task is what needs the connection held (its listeners arm over it, #817); the endpoint's
     line polls borrow the session instead of dialing, and a poll over a session that is not up
     faults (`collection-failed` naming the state) rather than dialing a connection of its own. The
     worklist carries each task's endpoint id, the node-unique key a session is held under.
  4. **Recovery is bounded.** A failed attempt waits `SessionBackoff`: 250ms doubling per attempt,
     capped at 30s, jittered 20% either way; five consecutive failed attempts read `down`, and the
     session keeps retrying at the capped pace so a device that returns reads `up` unattended. An
     attempt fails when its dial fails, when its connection drops unanswered within 10s (a device
     that accepts and drops cannot hot-loop or read `up` forever), or when an exchange over it that
     declared no reply shape timed out (a hung device, however long the connection lived; a shaped
     exchange's timeout fails only itself, ADR-0151). An answered exchange forgives the
     failures before it, so a healthy device's rare missed answer never adds up to `down`; a
     connection that outlives 10s and drops for another reason forgives them too and redials at
     once. Once `down`, a returning connection publishes nothing until it proves itself: an answered
     exchange (the session lends it to polls meanwhile), a line a listener claims (a push proves the
     device talks, though it forgives no failures, so a device that pushes and drops still backs
     off), or, unless answer timeouts were what failed, outliving the 10s window; so an accept-then-drop or hung device publishes no `up`/`down` pairs.
     A caller abandoning an exchange (its own deadline or cancel) is not the device's timeout, and
     the wait for a first dial has its own budget, so a slow dial never eats the exchange's. A held
     connection carries a 15s TCP keepalive, whose failure is recorded as itself.
  5. **An answer belongs to its ask.** One reader goroutine owns the connection: on an ask it
     discards every byte already received (a banner, a stray line, plus a 2ms window for bytes in
     flight), writes the line, and hands the next line it reads to that ask, keeping an answer whole
     across segments. An ask whose answer does not come in time recycles the connection, since its
     late reply would otherwise answer the next ask. One window remains: a line the device sends
     after the drain and before it has read the request (a push landing in that instant) is taken
     as the answer, since nothing on the wire tells the two apart. [ADR-0151](#adr-0151-a-held-line-session-routes-every-inbound-line-listeners-first-answers-by-reply-shape)
     (#817) closes it for every request that declares a reply shape, and a line a listener claims is
     never an answer at all. A listen task whose spec
     changes (its content-addressed id re-hashes) keeps the healthy connection; only what the
     connection depends on (transport, address, timeout, credentials) recycles it.
- **Context:** The nodes page designed a `session_log` table of node-reported transitions with a
  `session` view over it. A property series already gives transition-only history, latest-value
  reads, staleness at read time, rules over it (an event rule can watch `session-state` as it watches
  `endpoint-reachable`), and owner scope, with no new table, route, or permission noun; the free-text
  reasons belong to the node's own narration, which `node_log` already lands. A session table stays
  possible if per-session counters (commands, errors, last activity) earn one; nothing here forecloses it.
