---
title: "snmp's rung is a standard get answered, and only an authorizationError reads as auth"
date: 2026-10-24
status: "Accepted"
refs: [821]
summary: "`snmp` is v2c, built and stateless (`held: true` was honoured by no code path), its transport rung a get of a standard OID answered, `ladder.transport.request.get`, sysUpTime.0 unless the driver names another, sent with the endpoint's community, which the worklist now delivers to the probe (#821). Any response is an answer: responsive, one `snmp-response-time`, `endpoint-authenticated yes`, the verdict. A wrong community is dropped silently by the protocol and reads not responsive, retryable, indistinguishable from a stopped agent; only `authorizationError` is a terminal `auth` refusal at the session rung, beneath which no get is sent by the probe or a poll. The simulated faces are strict and answer a refused community with authorizationError"
---

- **Date:** 2026-10-24 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [nodes](/architecture/nodes/), [simulation](/architecture/simulation/), [glossary](/architecture/glossary/), [drivers](/guides/admin/drivers/)
- **Decision:** (#821, under the architect's approval of epic #1076, decisions 6 and 7a.)
  1. **Built and stateless.** `snmp` reads built in the transport registry and not held: the
     `held: true` it carried was honoured by no snmp code path. Its rung is `request`.
  2. **The rung is a standard get answered.** `ladder.transport.request.get`, by default
     `1.3.6.1.2.1.1.3.0` (sysUpTime.0), so every snmp driver gains its rung with no spec change
     (`ResolveLadder` supplies it, so the default is part of the resolved ladder the fingerprint
     covers). A driver may name another standard OID; a rung request that names a line or a path, or
     an OID that is not a numeric one, is refused at write and by the boot seed. The get plugs into
     the request-rung dispatch by transport that udp added
     ([ADR-0163](#adr-0163-a-udp-endpoint-binds-its-own-socket-and-its-rung-is-a-request-answered)),
     beside it: the existing `snmp-generic` poll path is unchanged and shares the getter.
  3. **The probe carries the community.** v2c has no unauthenticated request, so the worklist
     delivers the reachability probe the one credential its rung sends, the secret its driver's
     `community` input references (the poll's naming contract), and no other; every other probe still
     carries none. The secret's sealed version was already a fingerprint input, and the port rides
     the params; there is no version input to cover (v2c only, decision 6).
  4. **Any response is an answer.** A `noSuchObject` included, as any http status is: the rung reads
     responsive, lands one `snmp-response-time` (decision 7a, a `node-probe` key folded into `rtt` by
     #1063), and `endpoint-authenticated yes`, since a v2c agent answers only a community it accepts.
     With no port to open, the rung is the verdict and no `tcp-open` lands; the ping stays
     informational.
  5. **The v2c limit.** A wrong community is dropped silently by the protocol (RFC 3584): not
     responsive, `timed out`, retryable, never rejected, indistinguishable from a stopped agent, and
     the docs say so. Only an agent answering `authorizationError` refuses detectably: responsive,
     timed, `endpoint-authenticated no`, and a terminal `auth` refusal that gates the **session
     rung** (not the transport rung, unlike udp's, since the community authenticates). Beneath it
     nothing is sent under that fingerprint: the probe lands its held `no` and a stateless snmp poll
     is held too, since every v2c request carries the community. Rotating the community secret moves
     the fingerprint and brings the endpoint up within a tick. SNMP v3 is #1083.
  6. **The simulated faces.** A v2c face is strict: it judges the community itself (the library,
     configured with no security, answered any) and drops any but its own silently; it may declare a
     `refused_community`, answered with `authorizationError`. The catalog's faces answer `public`
     and refuse `revoked`.
- **Context:** #821's open question (a separate probe, or responsiveness derived from poll
  outcomes) was closed by ADR-0158 and the epic's ruling: the rung is the driver's standard request,
  run as the reachability probe every tick. The layered-gate design's `snmp.reachable` toggle and
  its `snmp_check` default retired into the ladder with udp's slice. A face that answered `public`
  beside its own community let a wrong-community test pass by accident.
