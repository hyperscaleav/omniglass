---
title: "Push ingest: the API telemetry lane ([#423](https://github.com/hyperscaleav/omniglass/issues/423))"
date: 2026-07-30
refs: [423]
form: bullet
---

- **Push ingest: the API telemetry lane ([#423](https://github.com/hyperscaleav/omniglass/issues/423)).**
  A second ingest lane beside the node's, so data can arrive without a driver, a node, or a protocol
  client. **`POST /telemetry:push`** takes telemetry in our own schema (a *webhook*, by contrast, is the
  case where we do not control the payload and something must normalize it, which is driver work) and is
  gated by **`telemetry:push`** with the caller's **scope as the fence**: the owner is declared in the
  body and read back through the gateway, so an out-of-scope owner is a non-disclosing 404. That is the
  inverse of the node lane, where the server must derive the owner because a node cannot be trusted to
  assert one.
  The payload is **two arrays, split typed versus untyped**: `samples[]`, where the **registry** decides
  the destination (metric, state, or a caught `event`), and `logs[]`, which are untyped and ungated. The
  caller supplies a name, never a table, so reject-not-project stays the only authority over where a
  value lands. Rejection is **synchronous** (validated at the route against the in-memory registry
  snapshot) while acceptance stays asynchronous, because a 202 that reveals nothing is useless to a human
  who mistyped a name.
  A push **publishes onto the data lane** (`og.v1.api.telemetry`) rather than writing Postgres directly,
  since the rule engine is designed as a stream consumer and anything skipping the stream would be
  invisible to every rule, alarm, and derivation. Trust is decided by **which subject a batch arrived
  on**: the lane sits outside the single-token `og.v1.telemetry.*` wildcard (a node named for any literal
  reserved there would otherwise be granted it), only the server's credential can publish to it, and a
  node-lane batch that asserts an owner is **dropped, not ignored**. Both lanes land through one
  extracted **`land()`**, so a new lane cannot drift from the node lane's semantics.
  Three proto fields earn their place: `owner`, `source` (the node lane fills provenance from
  `interface_type`, which a push has none of), and `Sample.instance` (a first-class dimension the wire
  could never express, since the node lane smuggles it via the interface name). Both fall back to the
  interface-derived value, so the node lane is unchanged. Still directional: owner kinds beyond
  `component` ([#422](https://github.com/hyperscaleav/omniglass/issues/422)) and webhook normalization.
