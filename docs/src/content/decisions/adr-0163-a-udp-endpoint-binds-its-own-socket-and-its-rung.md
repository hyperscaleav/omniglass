---
title: "A udp endpoint binds its own socket, and its rung is a request answered"
date: 2026-10-24
status: "Accepted"
summary: "`udp` is the line protocol as datagrams, built and stateless, its transport rung the driver's request answered (#1037). Each endpoint binds its own socket (the `listen-port` input, a fingerprint input, else a kernel pick) that its requests, replies and pushes share; a datagram from another source is counted, logged once per tick and dropped. The socket is unconnected with the error queue on, so a closed port reads `refused`. `request.retries` defaults to 2 for polls and the rung, none for commands; a terminal reply gates the transport rung. The rung is the verdict and lands one `udp-response-time` per answered probe, never `tcp-open` or `session-state`. The simulated face's loss is seeded per datagram"
---

- **Date:** 2026-10-24 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [nodes](/architecture/nodes/), [simulation](/architecture/simulation/), [glossary](/architecture/glossary/), [drivers](/guides/admin/drivers/)
- **Decision:** (#1037, under the architect's approval of epic #1076, decisions 5 and 7a.)
  1. **The line protocol as datagrams, stateless.** `udp` is built in the transport registry,
     not held, its rung `request`. A datagram carries one line; text payloads only, unicast only.
     There is no connection to hold, so no `session-state` lands: reachability is the request rung,
     every tick.
  2. **One socket per endpoint.** Each udp endpoint binds its own local port on the node, its
     driver's `listen-port` input (a number, 1 to 65535, derived into the endpoint's params as `host`
     and `port` are, so a fingerprint input), else a port the kernel picks. Its request rung, polls,
     commands and arm conversation all leave from it, so a reply and a push arrive on one socket with
     no table of sources. The operator configures the component and the node's firewall. A datagram
     from any source but the endpoint's component is counted, self-logged once per tick and dropped,
     never published. A shared node port routed by source address was rejected: it cannot tell
     apart two endpoints behind one address, the IP-to-serial gateway shape
     [ADR-0153](#adr-0153-the-held-session-proof-is-the-line-protocol-over-tcp-other-held-transports-are-their-own-issues)
     sends serial gear through.
  3. **Unconnected, with the error queue on.** #1037 asked for a connected socket so a closed port
     reads `refused`. A connected udp socket filters other sources in the kernel, and those must be
     counted, so the socket stays unconnected and the node turns on its error queue (`IP_RECVERR` on
     Linux), which reports the host's port-unreachable to the next read; the queue is drained on
     every report so none holds receive buffer. `refused` names a cause and never gates; elsewhere
     than Linux a closed port reads as a timeout.
  4. **The rung is the driver's request answered, and it is the verdict.** A udp driver must name
     `ladder.transport.request`. An answer (or the component's refusal by the reply's error shape)
     reads responsive; silence reads `timeout`. With no port to open, `endpoint-reachable` is the
     responsive rung, no `tcp-open` lands, and the ping stays informational. Each answered probe
     lands one `udp-response-time` (decision 7a; folded into `rtt` by #1063), an error-shaped
     answer included, timed from the first send, since a reply may answer any send, a `node-probe` key
     ([ADR-0161](#adr-0161-every-key-has-one-writer-class-held-at-the-storage-gateway)).
  5. **Retries are declared, and a command is sent once.** `request.retries` (udp only, 0 to 5)
     re-sends a lost datagram, the attempts sharing the endpoint's one timeout: 2 by default for a
     poll and the request rung, none for a command unless its binding declares them, since a lost
     reply must not send a relative command twice; an arm line is sent once too. The delivery
     carries the binding's retries. Every udp request declares its reply's shape (refused at
     write otherwise). An exchange whose answer came back sooner after its last send than half the
     component's latency (learned from sends answered at once) answered an earlier send, so it
     leaves a ghost that absorbs repeats of its answer for one more timeout, and a slow
     component's surplus reply never answers the next ask; a retry that loss caused leaves none,
     since a lost send draws no reply, so an ask sent once is never starved of an identical
     answer. A reply later than the whole timeout is not told apart.
  6. **Terminal replies gate the transport rung.** Over udp a driver's session block may declare
     only terminal replies. One that answers gates the endpoint at the transport rung whatever its
     class, since nothing logs in over udp (a transport whose rung is a request has no session
     rung), and beneath the gate no request, poll, command or arm is sent under that fingerprint,
     a node restarted onto a carried-back gate included; the first answered rung after it lifts
     arms.
  7. **Listeners ride the socket.** The listeners' instantiation commands are sent when the socket
     opens and again whenever the request rung recovers, since a component that rebooted forgot its
     subscriber; claims follow the held line connection's precedence.
  8. **The simulated face.** A persistent udp interface serves the line protocol per source
     address over the shell loop; the panel's `nvp-udp` face is the proving face, joined to the dev
     fleet with `newtron-nvp-udp` attached. Its loss (`loss_pct`) is **seeded per datagram**: the
     fate of a datagram is fixed by the device, the face, the direction, its bytes and how many
     times they have crossed, so a test on a lossy face repeats however traffic interleaves.
- **Context:** an open udp port cannot be detected, so the presence check the layered-gate design
  sketched (`udp.open`, disambiguated by the ping, `pingGates`) was never definitive; ADR-0158 put a
  named request on the ladder for this case, and this transport is its first user over datagrams
  (snmp's, #821, plugs into the same dispatch). A per-tick sequence of random draws made a lossy
  test depend on how probes and polls interleaved; a fate per datagram does not.
