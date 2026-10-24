---
title: "One connection fingerprint gates every connection, on a ladder the driver declares"
date: 2026-10-23
status: "Accepted"
summary: "One **connection fingerprint** gates every connection, held or stateless (#1056 ruling): a digest the server computes over every input a connection is made with (transport, declared params, driver inputs, each secret's sealed version, the session block, the ladder, the ssh pin, the `:retry` counter), held in `internal/fingerprint`. Connections are verified bottom up on the existing ladder (reachable, informational ping; responsive, the transport rung and the gate; authenticated, the login), declared in the driver spec with registry defaults. A terminal refusal gates its rung and above for the fingerprint; lower rungs keep running; the gate is published as `endpoint-rejection` and carried back so a restart honours it. **Amended (#1059, #1080):** the push lane refuses every native probe key by name, not only the gate and the host key"
---

- **Date:** 2026-10-23 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [nodes](/architecture/nodes/), [glossary](/architecture/glossary/), [drivers](/guides/admin/drivers/), [api](/architecture/api/), [properties](/architecture/properties/)
- **Decision:** The architect's rulings of 2026-10-23 (on #1056 and #1049), for every connection to
  an endpoint, held or stateless (the reachability probe, a throwaway poll or command, the held
  session):
  1. **The fingerprint primitive.** `internal/fingerprint` digests (SHA-256 over canonical JSON)
     every input a connection is made with: the transport, the declared params (the address as
     declared, never a DNS result, the port, the timeout), the driver's inputs (secret reference
     names, never values), a digest of each attached secret's sealed envelope and id (a rotation
     re-seals, so it moves; nothing plaintext-derived is hashed), the driver's session block, the
     resolved ladder, the ssh host key pin, and the endpoint's `:retry` counter. The server computes
     it in the worklist, one per endpoint, so the node only compares. A reflection test holds that
     every field moves it, and a table test that every field a task delivers is either one of its
     inputs or declared not one, so a new transport cannot read an input the fingerprint misses. It
     is never logged.
  2. **The ladder, bottom up, on the existing rungs.** `reachable` is a ping and **never gates**:
     an open, answering transport is what counts, so a component that blocks ping never reads down
     for it (the ping lands `icmp-reachable` beside the probe, one observation per probe, after the
     verdict). `responsive` is the transport rung and **the gate**: a TCP connect on the transport's
     port, the protocol's handshake (ssh key exchange, tls), or, where an open port cannot be
     detected (udp), the driver's named request answered (run as a line over tcp today; snmp's own
     request is #821, a udp datagram's #1037). `authenticated` is the session rung, the
     login, attempted only when the transport rung passes. A wrong host, address or port fails
     the transport rung, retryably, and never reads as auth.
  3. **Declared in the driver, defaulted by the registry.** The spec's `ladder` declares whether to
     ping (`ping.enabled`; a cloud API says no), the transport rung (`transport.rung`, with its
     `request`), and whether a session rung exists (`session.enabled`); the transport registry's
     `rung` supplies the defaults, so most drivers declare nothing, and a rung a transport cannot
     run refuses at write. Each rung is an object so per-family measurement toggles and a burst
     count and interval (#1063) and a path rung (#1064) join without reshaping it, and a layer-2
     presence rung (#1062) slots in below the ping, informational the same way.
  4. **The gate.** A terminal refusal (`auth` at the authenticated rung; `host-key`,
     `certificate`, `protocol` at the responsive rung) gates its rung and every rung above it for
     the endpoint while the fingerprint is unchanged; the rungs below keep running, so reachability
     stays observed while auth is rejected. The held session starts rejected, the ssh probe sends no
     credential (#1060: tried once, never past a pinned key that changed), a stateless poll faults
     without fetching, and a command fails fast. A changed fingerprint lifts the gate, and so does
     the rung passing. The #1056 login budget (three silent hang-ups after the secret) feeds it.
  5. **Restart safety.** The gate is published as the `endpoint-rejection` property (`<rung>
     <class> <fingerprint>`, or `none`), republished every pull until the worklist reflects it, and
     carried back, so a restarted node makes no attempt against an unchanged fingerprint. A refusal
     reported for an attempt under an older fingerprint gates nothing. The ssh pin the node trusted
     on first use is a learned fact and stays out of the fingerprint; only the operator's declared
     pin goes in. `endpoint-rejection` and `ssh-host-key` are node control facts the push lane
     refuses, so no push can plant or lift a gate. **Amended (#1059):** `session-state` and
     `session-reason` join them, since the console reads them as the node's account of its own
     connection: a push could otherwise read a rejected session up, hiding the refusal and its
     Retry, or plant a reason the node never scrubbed. **Amended (#1080):** every native probe key
     joins them. The reachability verdict and its rungs, the probe metrics and their rtt-style
     timings are read by the console as the node's observation too, so they are no more a push's to
     write than the gate. The refused set is `internal/collection/probekeys` itself, where each key
     is ruled node-only or pushable with the reason it may be pushed; none is pushable today, and a
     test fails on a key declared there and ruled neither, so it cannot ship pushable by omission. The
     refusal holds on every lane (a native name pushed as an event is refused as the node's, not as
     unregistered), under any instance and any casing, and its reason names the key. The node's own
     lane never reads the ruling, so it writes every one of them as before. **Amended (#1078):** a refusal of a changed
     key appends its evidence, `<pinned-key> <offered-key>`, to the gate's value
     ([ADR-0159](#adr-0159-a-refused-keys-evidence-rides-its-gate-and-trusting-a-key-is-checked-against-the-one-offered)). **Amended
     ([ADR-0160](#adr-0160-tls-is-its-own-transport-over-a-shared-primitive-and-its-pin-is-the-public-key)):**
     a tls certificate refusal for a changed key carries the same evidence; the tls keys
     (`tls-certificate`, the handshake time and the certificate facts) are node-only, and a driver
     may not emit a node-only key either. **Amended (#1110):** the gate publishes its reason beside
     it as `endpoint-rejection-reason`, node-only, in the same batch, held session or not; an adopted
     gate publishes none
     ([ADR-1110](#adr-1110-a-gate-says-why-it-holds-and-the-console-shows-it-whatever-met-the-refusal)).
  6. **Connection inputs edit in place** (the architect's ruling on #1061). A driver-attached
     endpoint's host, port and attached secret references are patched through `PATCH
     /endpoints/{id}` `inputs`, validated against the driver's declared inputs (a refused value is a
     400 naming the field), its tasks re-derived in the same transaction through #823's derivation,
     audited; a transport is accepted only where the driver speaks it. Re-attach is not the path.
     The confinement the earlier read-only rule bought (a credential never sent to an address its
     attach did not name) is kept another way: the edit re-resolves every attached secret under the
     editor's own secret scope and admin tier, so an editor who could not use a credential cannot
     redirect it. The fingerprint moves, so a rejected session un-gates and a healthy one recycles
     onto the new address within a tick.
- **Context:** A refusal retrying cannot fix must stop every path that would retry it, not only the
  held session: the ssh probe retried a params credential every tick (#1060), and a restarted node
  forgot every rejection. Gating on "what changed" needed one definition of the inputs, computed
  where the sealed secrets live so a published digest leaks nothing about a value. ICMP is blocked
  on much installed gear, so only a transport answer can say a component is there. Follow-ons: the
  udp request rung's snmp proof is #821; per-rung rtt, jitter and loss as driver toggles, stored as
  one observation per probe with statistics from windowed calc rules, are #1063 and #1065; a path
  rung (hop count, network path) is #1064; layer-2 presence is #1062.
