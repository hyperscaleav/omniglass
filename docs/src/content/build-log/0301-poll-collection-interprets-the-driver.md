---
title: "Poll collection interprets the driver"
date: 2026-10-17
refs: []
form: bullet
---

- **Poll collection interprets the driver.** The compiled unit the attach bakes (function, request,
  emits with lanes) moves to the shared `internal/driver` package and gains its interpreter:
  locate each emit by its one extractor (OID, regex capture, dotted JSON path, key), apply the
  declared transform (enum map, then scale, then cast), type by the baked lane. The contracts the
  tests pin hardest: a metric-lane value that is not numeric is a fault, never a wrong-lane
  sample; a missing emit faults by name while the rest still land; an unmapped enum value refuses
  rather than projecting the raw through; a failed fetch lands nothing at all. The node's
  dispatch tells a baked driver poll from a probe task by its spec and publishes each fault as a
  `collection-failed` event beside whatever landed; a standing listen task is skipped inert until
  the stateful arc arms it. Fetching is two new capability wrappers, each proven on real wires:
  the v2c scalar SNMP GET (`gosnmp`) against a hand-rolled from-the-BER-spec UDP agent (request-id
  correlated, community adjudicated with silence for a wrong one, an OctetString sysDescr beside
  a TimeTicks sysUpTime), and a stateless line exchange against a real listener. Credential
  delivery closes the loop: the worklist reply resolves a driver task's secret inputs and ships
  them unsealed on the per-node subject (the node's NATS grant is the boundary; the attach is the
  audited operator action, so machine delivery writes no per-pull audit row), and a reference
  that no longer resolves is delivered as absence, which the node turns into a
  `collection-failed` naming the missing credential (#814, #603).
