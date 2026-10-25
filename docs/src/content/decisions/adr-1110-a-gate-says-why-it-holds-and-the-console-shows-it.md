---
title: "A gate says why it holds, and the console shows it whatever met the refusal"
date: 2026-10-24
status: "Accepted"
refs: [1110]
summary: "A gate publishes its reason beside it (#1110, amending ADR-0158 point 5): `endpoint-rejection-reason`, the refusal's class and the component's own text, node-only, written in the same batch as `endpoint-rejection` whenever the node records a gate, held session or not; a gate adopted after a restart writes none. The reachability read serves `rejection` with its `reason` and `ts` whether or not a session is held, and the console's refusal notice keys on the gate, so a probe's refusal (a tls certificate, a udp terminal reply, an snmp authorizationError, the ssh probe's credential) shows the same reason, Retry and Trust this key as a rejected session, a retry waiting on the gate where no session exists"
---

- **Date:** 2026-10-24 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [nodes](/architecture/nodes/), [api](/architecture/api/), [glossary](/architecture/glossary/), [drivers](/guides/admin/drivers/)
- **Decision:** (#1110, the architect's approval in session.)
  1. **The gate carries its reason.** The node publishes `endpoint-rejection-reason` beside
     `endpoint-rejection`, in the same batch and at the same time, whenever it records a gate
     (`<class>: <the component's text>`, as the refusal reads; the same text a held session's
     `session-reason` carries, scrubbed of anything the node sent). It goes out again with the gate
     until the worklist reflects it. A gate the node adopts from the worklist after a restart never
     saw its refusal, so it publishes no reason and the one on record stands, and a held session
     started rejected under it does not re-record it from its own replay (told by the refusal's
     identity, not its value); a driver's re-probe that meets a refusal under the same fingerprint
     records its words, and the reason goes out again beside the unchanged gate. A gate that lifts
     publishes `none` and no reason. The key is native and node-only, so no push can plant a reason
     the node never scrubbed.
  2. **The read serves the gate whatever holds a session.** `rejection` already ignored the session;
     it now carries `reason` (read only while a gate holds, so a lifted gate's last reason is never
     served, cut to 500 characters like a session's) and `ts`, when the gate was last published.
  3. **The console keys the notice on the gate.** `SessionNotice` shows for a rejected session or a
     held gate, its reason the gate's, else a rejected session's (one published before the gate
     carried a reason), else the class: a session restarted under an adopted gate carries the
     node's placeholder as its reason, while the gate keeps the refusal's own words. Retry (behind `endpoint:retry`) and Trust this key wait on what they
     move: the session's transition time where a session is held, else the gate's publish time.
     There is one notice, not a second one for probes.
- **Context:** A probe's refusal gated the endpoint (ADR-0158) and the ladder showed the held rung,
  but the notice, its reason and Retry keyed on `session.state === "rejected"`, and an endpoint gated
  by its reachability probe alone holds no session (the snmp community refusal found in #821, a udp
  terminal reply, a tls certificate refusal, an ssh probe's credential). Its reason had nowhere to
  live: `session-reason` is a held session's, and the tls probe only logged it once. Keying the
  notice on the gate alone would have shown a refusal with no why, so the gate gained its reason.
