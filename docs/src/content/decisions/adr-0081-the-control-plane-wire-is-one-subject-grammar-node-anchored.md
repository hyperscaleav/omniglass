---
title: "The control-plane wire is one subject grammar, node-anchored and batch-granular"
date: 2026-08-06
status: "Accepted"
summary: "the control-plane wire is one subject grammar, og.v1.verb.node with the node name exactly one token: the api.telemetry lane sits in its own segment, per-record subjects are rejected, and the core-NATS consumers (worklist, heartbeat) are singletons by construction with their HA fork named and deferred"
---

- **Date:** 2026-08-06 | **Status:** Accepted | **Pages:** [messaging](/architecture/messaging/),
  [scaling](/architecture/scaling/)
- **Decision:** every control-plane subject is `og.v1.<verb>.<node>`, the node name the last token
  and **exactly one token**, so the server subscribes per-verb single-token wildcards and a node's
  credential is an explicit allow-list of its own subjects plus its private `_INBOX.<node>` reply
  namespace. The verb family is `worklist`, `heartbeat`, and `telemetry`, with `worklist-changed`
  reserved for the re-pull nudge and `og.v1.command.<node>` the committed future per-node command
  queue. The trusted push lane is **`og.v1.api.telemetry`, its own segment**; the rejected
  alternative was a reserved node name under `og.v1.telemetry.*`, where the single-token wildcard
  would hand a node named `api` the trusted subject, so its own segment makes the forgery
  structurally impossible rather than dependent on nobody choosing an awkward name. Addressing is
  **node-anchored and batch-granular**: a record's name is payload, never topic, and per-record
  subjects (the MQTT-style topic tree) are rejected. One recorded correction rides the entry: the
  one-token name rule (#586) was never justified by names as topic tokens; it stands on its own
  grounds. One consequence is named and deferred: the core-NATS verbs' server-side consumers
  (worklist, heartbeat) are singletons by construction, and their HA fork (queue groups versus
  worklist reassignment) is a scaling decision for the day a second server exists. Telemetry does
  not face that fork: its ingest is a named durable JetStream consumer a second server joins.
- **Context:** the topics conversation (recorded until now only in #584's issue body) weighed a
  user-facing MQTT-style topic tree against the internal contract and split the two concerns: the
  internal bus follows KISS (NATS subjects carrying batches between the node and the server), and
  any future user-facing subscription surface is its own design with its own grammar, reachable
  over an MQTT bridge if wanted. Wire constants live in `internal/collection/wire.go`; the grammar
  section on [messaging](/architecture/messaging/) is the page-of-record.
