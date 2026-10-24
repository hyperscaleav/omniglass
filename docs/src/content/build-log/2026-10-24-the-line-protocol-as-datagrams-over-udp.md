---
title: "The line protocol as datagrams over udp (#1037)"
date: 2026-10-24
refs: [1037]
---

The second slice of the protocol wave (#1076), under the architect's approval of its decisions 5
and 7a ([ADR-0163](/architecture/decisions/#adr-0163-a-udp-endpoint-binds-its-own-socket-and-its-rung-is-a-request-answered)).
`udp` is built in the transport registry, stateless, its rung a request. The datagram primitive,
`collection.DatagramConn`, is one bound, unconnected socket per endpoint with the error queue on
(`IP_RECVERR`, drained on every report): an exchange takes its reply by the declared shape with the
held line connection's precedence, re-sends a lost datagram within the one timeout, reads a closed
port `refused`, and counts and drops a datagram from any other source. The driver spec gains
`request.retries` (udp only, 0 to 5; 2 for a poll and the request rung, none for a command unless
its binding declares them) and the `listen-port` input (udp, a port), which the attach derives into
the endpoint's params, so editing it moves the connection fingerprint and a bare endpoint's is
checked at write; a udp driver must name its request rung and may declare only terminal replies in
its session block. The node holds each udp endpoint's socket beside its sessions: the request rung,
polls, commands and the arm conversation leave from it; the rung lands `endpoint-responsive` and one
`udp-response-time` per answered probe (writer class `node-probe`, [ADR-0161](/architecture/decisions/#adr-0161-every-key-has-one-writer-class-held-at-the-storage-gateway)), never `tcp-open` or `session-state`, and decides
the verdict where no port can be opened; a terminal reply gates the transport rung; a recovered rung
re-arms the listeners; another source's datagrams are self-logged once per tick. A command delivery
carries its binding's retries. The console's breakdown lists a udp endpoint's request rung (the
layer its verdict follows), and its ladder hint and why-line teach it. The
simulated panel serves NVP as datagrams (`nvp-udp`, 51327, per source address over the shell loop),
dropping a share of datagrams each way, seeded per datagram so a lossy run repeats; the dev fleet
attaches the new official `newtron-nvp-udp` to the huddle panel.

Proven over real sockets (`udp_integration_test.go`: the shaped answer beside a push, the error
shape, retries within the timeout, a command sent once, a refused port, a stranger counted, a
terminal reply, the listen port), on the node against a real component socket (`datagram_test.go`:
the rung and its verdict, refused, the gate, a poll through retries, commands once unless declared,
arm at open and on recovery, the foreign flush, rebinding), against Postgres (`udp_test.go`: the
listen port as a fingerprint input, a bare endpoint's check, a delivery's retries), and end to end
against the simulated face dropping 30% each way (`run_udp_test.go`), passing with two runs at once.
The layered-gate fence's `udp.open`, `pingGates` and per-check toggles retire into the ladder.

The review's fixes, each with a test that failed first: a gate carried back to a restarted node now
holds the arm too, which had subscribed to the component that refused (the first answered rung after
the gate lifts arms); an exchange whose answer came back too soon after its last send to be that
send's reply leaves a ghost absorbing repeats of it, since a slow component's surplus reply had
answered the next ask (a command took a poll's reply), while a retry loss caused leaves none (the
re-review caught a first cut that starved a command's identical ack), and every udp request now
declares its reply's shape; `udp-response-time` is timed from the
first send, since a reply may answer any send and the later send had read a slow component fast; an
arm line is sent once, never once per retry; a gated request rung reads refused beneath the gate in
the console, not unanswered; the tests pick no released port (a privileged port is the closed one,
the end-to-end test reads the node's own port back from the panel, a listen port is picked again on
a collision); and the nodes and simulation pages stop calling udp Design and an echo. Two catalog
entries record the classes: `connectionless-late-answer` and `side-send-skips-gate`.
