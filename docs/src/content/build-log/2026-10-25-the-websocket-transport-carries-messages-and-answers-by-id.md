---
title: "The websocket transport carries messages and answers by id (#1034)"
date: 2026-10-25
refs: [1034, 1113]
---

The fourth and last slice of the second protocol wave (#1113), which completes the epic
([ADR-1034](/architecture/decisions/#adr-1034-a-websocket-is-a-held-session-of-text-frames-and-a-reply-is-matched-by-a-minted-id)).
`websocket` and `wss` join the registry as held line transports, `wss` over the shared TLS primitive
with its key pinned as `tls` and `https` pin theirs. A request is a frame (a line, or a JSON body
rendered injection-safe), and a reply may `correlate` by a field: the node mints a random id into
it each time it sends the frame, and the answer is the frame carrying it back
(`internal/driver/websocket.go`).

The collection engine's websocket dialer (`internal/collection/websocket.go`) is the session
machine's staged dialer: it connects (and runs the handshake over wss), logs in first when the
session declares an http login, and sends the upgrade with the scheme's credential, the login's
session and its cookies through a bounded head; a 401 or 403 to it is a terminal `auth`. A reader
bounds each frame, a router owns every data frame written and routes what it reads by the line
session's precedence, and a pinger keeps the socket proven. The node holds the endpoint's socket as
its session, its upgrade sharing the endpoint's learned auth, binds each correlated poll and command
at execute, and probes it as http and https are probed. An adversarial review with a security lens
(upgrade auth, origin, frame limits) found three blocking faults, each fixed test-first: a frame
read just before the component's close could be lost (now routed in order before the connection
dies), a re-probe could only replay the gate (refused at validation), and a status-scoped terminal
rule judged a frame's text (it now judges the upgrade alone).

The simulator gains a `websocket` face, a JSON control socket behind its own auth with subscribe,
get and set and a console drop; the Kestrel bar serves one over wss on 9443, and the seeded
`kestrel-ws` driver holds it: the subscription armed on connect, the pushes landed by listeners, the
polls and the `set-volume` and `set-mute` commands answered by their ids.

Proven by the driver's validation and correlation tests, over real sockets (an arm on connect, an
exchange answered by its id past a push and another id's answer, a drop recovered and armed again, a
wrong credential refused once whether 401 or 403, each scheme and a form login, the keepalive, an
oversize and a binary frame dropped, a pinned key, the head bound), in the simulator's face, and end
to end (`run_websocket_test.go`: a change on the bar lands within a second, a command settles from
the answer to its id, a drop recovers and arms again, a wrong password reads rejected after one
upgrade across a restart, and a replaced bar's key is held until `:retry` trusts it).
