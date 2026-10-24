---
title: "A node is a kind=node principal with an interim bearer credential and static per-connection NATS subject permissions"
date: 2026-07-07
status: "Accepted"
summary: "A node is a `principal` of `kind=node` with a 1:1 detail table and a bearer `credential` row (interim shared secret), and per-node NATS isolation is static per-connection subject permissions via an in-process auth callback; nkey/JWT deferred"
---

- **Date:** 2026-07-07 | **Status:** Accepted | **Pages:** [nodes](/architecture/nodes/), [identity and access](/architecture/identity-access/)
- **Decision:** A node is a first-class `principal` of `kind='node'` with a 1:1 `node` detail table (keyed by
  `principal_id`, alongside `human` and `service`), exactly as [identity and access](/architecture/identity-access/)
  describes. Its `name` is `not null unique` on the detail table and stays the estate address the collection FKs
  (`interface.node_name`, `task.node_name`, `metric_datapoint.node_id`) reference. The node runtime ships with
  two deliberate calls that diverge from the present-tense design, both reversible in a later hardening slice.
  (1) The node's credential is a **bearer `credential` row** on its principal, minted, stored (only as
  `sha256`), and verified through the **same helpers a service bearer token uses** (`AuthenticateBearer`), and
  the enrollment token **doubles as the node's NATS password** (a shared secret), rather than being a single-use
  bootstrap exchanged for a distinct long-lived credential. The decentralized **nkey/JWT operator-account**
  model that identity and access describes for nodes (a `nats` credential kind, a signed nonce, a JWT carrying
  the node's subject permissions) is deferred; the `credential` kind CHECK is **not** widened for it here. (2)
  Per-node NATS isolation is **static per-connection subject permissions**: the embedded `nats-server` runs an
  in-process `CustomClientAuthentication` callback that resolves each connecting node by name, verifies its
  bearer credential, and registers a user whose publish/subscribe grants are scoped to that node's own
  `og.v1.*.<node>` subjects, so a node cannot publish or pull as another.
- **Context:** Checkpoint 2 of the reachability slice needed a real, negatively-tested per-node isolation
  mechanic against an embedded server, without carrying the full JWT/nkey machinery a single slice should not.
  The auth-callback path adds per-node users **dynamically at enrollment time with no config reload**, which is
  the simplest mechanism that keeps the isolation invariant real: the negative test proves node A cannot use
  node B's subjects (and a confused-deputy reply cannot forge another node's liveness), and a wrong credential
  is rejected at connect. The subject encodes the node name in its last token and the callback grants only that
  node's subjects, so the subject **is** the transport isolation boundary (the payload-owner admission fence is
  a later checkpoint). Modeling the node as a `kind=node` principal (rather than the standalone table an earlier
  checkpoint built) puts it on the shared identity spine from the start: it has a real `principal_id` so it can
  be an audit actor, its credential rides the audited human/service machinery, and only the credential *scheme*
  (interim bearer vs nkey/JWT) remains to tighten. JetStream is enabled on the server now (it boots and shuts
  down cleanly), but the control-plane messages (worklist, heartbeat) are JSON over core NATS; the protobuf
  telemetry `Event` over JetStream is the next checkpoint.
- **Closes the gap:** the nkey/JWT node identity (the `nats` credential kind and the signed-nonce admission)
  and the single-use enrollment token are tracked with the node-identity hardening slice.
