---
title: "A probe-gated endpoint shows its refusal and offers a retry (#1110)"
date: 2026-10-24
refs: [1110]
---

An endpoint gated by its reachability probe alone (a tls certificate refusal, a udp terminal
reply, an snmp `authorizationError`, the ssh probe's refused credential) holds no session, so the
console showed the held rung on its ladder and nothing else: the notice, its reason and Retry keyed
on a rejected session, and the node's reason for the refusal was logged and lost
([ADR-1110](/architecture/decisions/#adr-1110-a-gate-says-why-it-holds-and-the-console-shows-it-whatever-met-the-refusal)).
The node now publishes `endpoint-rejection-reason` beside `endpoint-rejection` in one batch,
whenever it records a gate (held session or not), republished with the gate until the worklist
reflects it; a gate adopted after a restart publishes none, so the refusal's own words stand. The
key is seeded and node-only. The reachability read serves `rejection` with its `reason` and `ts`
whether or not a session is held, the reason only while a gate holds. `SessionNotice` keys on the
gate (`endpointNotice` in `lib/session`): the same notice, reason, Retry behind `endpoint:retry`, and
Trust this key for a changed key, with no session pill, a retry waiting on the gate's publish time
where no session exists (`retryMark`), captured when the retry is asked for. The Endpoints panel
renders its notice slot only for a row with something to say, and the Collection card reads `polls
stopped` for a gate that holds the polls (`pollsHeld`, the node's stateless rung: any gate at the
transport rung, and snmp's at the session rung). The dev seed gains a probe's gate: the
auditorium's touch panel's `snmp-generic` endpoint reads the community the panel's agent refuses,
which the `fix-a-refused-probe` walkthrough fixes from the console.

Proven by tests that failed first: on the node (`TestAGatePublishesItsReasonBesideIt`,
`TestRejectionSamplesCarryTheReasonBesideTheGate`, and the adopted gate's reasonless republish,
mutation-checked), end to end against the simulated snmp face (`run_snmp_test.go`: the reason lands
beside the gate and survives a node restart), on the read for each gated transport with no session
held (`TestReachabilityServesAProbeGateWithNoHeldSession`: tls, udp, snmp and ssh, the reason cut at
500 characters, a lifted gate serving none, a viewer reading the same gate while its retry is
refused 403), in the dev seed (`TestSeededFleetHoldsAProbeGate`: the secret holds the community the
simulator's profile refuses, nothing holds a session, a second run adds nothing), and in the
console (the notice, Retry, the denied viewer and the re-gated retry for each transport; the
Endpoints row, the Collection card and the endpoint's blade; Trust this key for an ssh probe's and a
tls probe's changed key).
