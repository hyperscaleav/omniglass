---
title: "A session holds its connection (#816)"
date: 2026-10-23
refs: [816]
---

The first slice of epic #603's stateful arc. A stateful transport keeps its connection across ticks:
`internal/collection/session.go` is the machine (establish, operate, recover, teardown) over a
`HeldConn` seam, and `internal/collection/linesession.go` is the real held line connection (one TCP
connection, a single reader goroutine framing lines and handing the next one to the exchange waiting
for it, a line nobody asked for dropped as unsolicited until listeners claim it in #817, an
unterminated flood bounded, a 15s keepalive). The node (`internal/node/session.go`) holds one session
per endpoint whose worklist carries a standing listen task over a transport with a session dialer (the
line protocol over `tcp`), keyed by the endpoint id the worklist now carries beside its name, and
reconciles the held set on every pull: a new listen task starts one, a removed task or a re-addressed
endpoint tears it down, node shutdown tears down all. The endpoint's line polls borrow the held
connection instead of dialing per tick; a poll waits only while the first dial is in flight, and over a
session that is otherwise not up it faults at once naming the state (a down device never stalls the
tick), never dialing on its own. Recovery is bounded by named constants: a
jittered exponential backoff (`SessionBackoff`: 250ms doubling, 30s cap, 20% jitter), five consecutive
failed dials to `down` (still retrying at the cap), and churn (a connection dropping within 10s) growing
the wait so an accept-and-drop device cannot hot-loop. The honest states land as the seeded
`session-state` property per endpoint (`establishing`, `up`, `recovering`, `down`), published at each
transition under the listen task, and each transition is a node self-log carrying the reason; there is
no session table ([ADR-0150](/architecture/decisions/#adr-0150-a-held-sessions-state-is-a-property-series-and-node-logs-not-a-session-table)).
Proven at three tiers: the state machine over fakes (`internal/collection/session_test.go`), the real
held line connection over a real socket (`linesession_integration_test.go`) and against the
simulator's NVP listener (`internal/sim/session_hold_test.go`: three polls ride one connection, the
listener killed mid-session reads recovering, then up once it returns), and end to end
(`internal/node/run_session_test.go`: the shipped `newtron-nvp` driver attached, the real node and
bus, the `session-state` series on the server reading establishing, up, recovering, up through a
listener outage, the reason in `node_log`, and node shutdown leaving the device holding no session).
