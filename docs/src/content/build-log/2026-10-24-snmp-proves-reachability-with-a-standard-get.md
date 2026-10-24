---
title: "snmp proves reachability with a standard get (#821)"
date: 2026-10-24
refs: [821]
---

The third and last slice of the protocol wave (#1076), under the architect's approval of its
decisions 6 and 7a ([ADR-0821](/architecture/decisions/#adr-0821-snmps-rung-is-a-standard-get-answered-and-only-an-authorizationerror-reads-as-auth)).
`snmp` reads built and stateless in the transport registry. Its transport rung is a get of a
standard OID answered, `ladder.transport.request.get`, sysUpTime.0 by default (`ResolveLadder`
supplies it, so `snmp-generic` gains its rung with no spec change), validated at write: a line or
a non-numeric OID is refused. The worklist delivers an snmp endpoint's reachability probe its
community, the one credential its rung sends. The getter reads an `authorizationError` answer as a
terminal `auth` refusal and a silent drop as a timeout. On the node the rung dispatches beside
udp's: an answer lands `endpoint-responsive`, one `snmp-response-time` (node-probe) and
`endpoint-authenticated yes`, and decides the verdict, with no `tcp-open`; an `authorizationError`
gates the session rung, beneath which neither the probe nor a poll sends a get under that
fingerprint; silence reads `timed out` and says nothing of the community. The reachability read
serves the rung as a `request` layer timed by `snmp-response-time`, and the panel names it the get,
its why-line naming the v2c limit. The simulated v2c faces judge the community themselves (the
library, unsecured, answered any) and answer a declared `refused_community` with
`authorizationError`; the catalog's faces refuse `revoked`.

Proven against a BER agent of the test's own (`snmp_integration_test.go`: an authorizationError a
terminal auth refusal, a wrong community a retryable timeout), against the simulated faces
(`snmp_community_test.go`), on the node over a fake getter (`snmp_rung_test.go`: the rung and its
verdict, the gate at the session rung holding the probe and a poll, a rotation lifting it), against
Postgres (the probe task carries the community alone), on the reachability read, and end to end
(`run_snmp_test.go`: the get answered and timed beside the poll, a wrong community silent and never
rejected, the refused community gated with no further get until the secret rotated, a stopped agent
down), with loss injected through the impairments under NET_ADMIN (the netadmin end-to-end test's
snmp leg reads down under 100% loss and up once it lifts).
