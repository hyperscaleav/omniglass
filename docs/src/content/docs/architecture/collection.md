---
title: Collection
description: "The flow engine: a versioned component template compiles to a per-node runtime unit that runs a DAG of steps over decoupled interfaces and parses at the edge."
---

Collection is a **flow engine**. A versioned YAML `ComponentTemplate` compiles to a per-node
runtime unit that executes a **DAG of steps over decoupled interfaces**, parses at the edge,
and writes resolved datapoints straight to the typed tables (with the raw payload kept as a
debug sidecar). One authoring schema covers everything from a single SNMP read to a
multi-step, cross-interface, branching pipeline.

## Model overview

Three strongly decoupled levels, plus a typed parameter surface and template metadata:

```text
ComponentTemplate (apiVersion, kind, metadata.labels)
  inputs       typed parameters; required = the :apply gate
  interfaces   connections, declared once, may be persistent/stateful, own liveness
  flows        each = one trigger + a DAG of steps
    steps      platform-function nodes (kind), gated by an interface, schema-validated
```

- **Authoring compiles to a runtime unit.** The hand-authored template is the contract. A
  compiler lowers it to the per-node execution unit the node runs: it resolves inputs and
  variables, validates the DAG, and bakes each datapoint's `kind` into the unit so the edge
  writes to the right table with no runtime registry lookup. The runtime unit is internal,
  never hand-authored.
- **Edge-local execution.** A flow runs per component on the node, in one tick, with zero
  server round trips: every interface sits on the one component, all reachable from the node,
  so a step can branch on a value a prior step just collected, straight from node memory.
- **Two data planes, split by access pattern.** Timeseries [datapoints](/architecture/taxonomy/)
  (observed and calculated) are append-heavy and history-bearing. Current-value config and
  secrets live in the separate [variable](/architecture/variables/) table (sargable
  point-lookups). A variable may link a datapoint as its observed side.
- **Kubernetes-style versioning.** `apiVersion: collection.omniglass.dev/v1alpha1` plus a
  `kind` (`ComponentTemplate`, later `SystemTemplate` / `LocationTemplate`). The parser gates
  on `apiVersion` and converts older versions forward.

## Interfaces: connections, declared once

A top-level `interfaces` array, each a named connection. The connection is **decoupled from the
work**: a flow's steps reference an interface by `id`; the interface owns the connection, not
the step. Declaring it once removes per-step duplication, and the decoupling lets a
**persistent session outlive any single flow run**, so subscriptions and inbound streams attach
to a connection established once.

```yaml
interfaces:
  - id: snmp
    type: snmp                     # interface_type registry entry (built flag + param schema)
    host: ${input.ip}              # references INPUTS, not $var: directly (see Inputs)
    version: "2c"
    auth: ${input.snmp}            # snmp_community shape; community field is secret (masked, audited)
    liveness: { oid: 1.3.6.1.2.1.1.3.0 }   # reachability gate, per interface
  - id: cli
    type: ssh
    host: ${input.ip}
    credentialRef: ${input.ssh}    # ssh_credential shape, bound to a $var: at apply
    persistent: true               # stateful session, outlives flow runs
```

- **Type is an `interface_type` registry entry** with a `built` flag (`snmp`, `http`, `ssh`,
  `telnet`, `tcp`, `icmp`, `webhook` built; `mqtt`, `syslog`, `websocket` coming). The per-type
  connection-param schema is registry-driven, so config lints against exactly what is built.
- **`liveness`** is the per-interface reachability gate; it decides whether the interface's
  flows run. See [nodes](/architecture/nodes/).
- **`persistent: true`** keeps a session open across flow runs (interface lifecycle contains a
  flow run, which contains a step). Schedule-flows borrow it to send; listen-flows wake on its
  inbound.
- **Codec and framing.** Raw-TCP AV control planes wrap payloads non-trivially (line
  terminators, length prefixes, NUL framing, JSON-RPC or TTP envelopes). An interface carries
  encode/decode controls to lock raw to shape; basic framing ships first, the richer codec
  layer extends over time.
- **Node placement is not declared here.** It is server-assigned from the component's location.

## Flows: a trigger plus a step DAG

A top-level `flows` array. Each flow is **one trigger and a DAG of steps**, from trivial (one
SNMP step reading 20 OIDs) to a multi-step branching pipeline.

A **trigger** is one of three kinds, and unifies what used to be split into pollers and
listeners: a poller is a schedule-triggered flow, a listener is an event-triggered flow.

| Kind | Fires when | Notes |
|---|---|---|
| `schedule` | an interval elapses, or `onStart` once when the interface comes up | `onStart` arms subscriptions |
| `listen` | inbound data arrives on a `source` | `source`: `webhook` / `trap` / `syslog` (server-hosted), or `subscribe` / `stderr` / `session-line` (bound to a persistent interface) |
| `manual` | invoked on demand (an operator, or a command by id) | no schedule; for debug ops |

`triggers` is modeled as a list; the first phase enforces exactly one. The foreseeable
multi-trigger case is a flow that fires on `schedule` and is also command-invocable for a
targeted refetch.

## Steps: the DAG

A step is a **platform-function node**: a `kind` (the function) that runs on a referenced
`interface` and produces datapoints through a typed extractor.

```yaml
steps:
  - id: poll
    interface: snmp
    kind: snmp.get                  # gated by the interface type; schema-validated
    when: "$dp.power.state == 'on'" # optional guard = explicit branch
    datapoints:
      oid:
        - { key: cpu.utilization, oid: 1.3.6.1.4.1.55540.2.1.0, value: "raw / 100.0" }
```

- **Dependencies are data references, not array order.** A step reads `$steps.<id>.*`
  (ephemeral scratch: a session id, a token, a list element, never emitted) or `$dp.<key>` (a
  real measurement, emitted and readable for branching). The set of references *is* the DAG;
  array order is cosmetic, so a flow editor can round-trip the graph.
- **`when`** is the explicit branch: an expression guard over the in-scope context. A false
  guard skips the step and its dependents.
- **`forEach`** is the step-level fan-out: a step iterates a located collection, the element
  bound as `$steps.<id>.item`, and downstream steps run per element (a list-then-detail chain).
  Distinct from an extractor's `each`, which fans one response's array into many datapoints
  inside a single step.
- **`kind` is interface-gated and registry-driven.** Valid kinds depend on the target
  `interface_type` (`snmp.get`, `snmp.walk`, `http.request`, `ssh.send`, `ssh.subscribe`, the
  interface-agnostic `extract` and `blend`). Each kind's param schema lives in the registry,
  built one kind per increment as adapters ship.

In-scope reference namespaces within a flow tick: `$var:<key>` (config and secret values,
resolved through the [cascade](/architecture/cascade/)), `$dp.<key>` (datapoints), `$steps.<id>.*`
(ephemeral scratch), `$event` (the inbound payload of a `listen` flow), and the extractor-local
inputs a step prepares for its `value` leaf (`raw`, `groups`, `node`, `item`).

### Extractors: locate, then optionally transform

Each extractor is a typed section that locates a raw value with its protocol-specific field,
then optionally transforms it with a single [Expr](/architecture/expressions/) expression in
`value` (default identity).

```yaml
datapoints:
  oid:
    - { key: device.uptime, oid: 1.3.6.1.2.1.1.3.0, value: "raw / 100.0" }  # centiseconds to seconds
  regex:
    - { key: fan.speed, match: 'fan \(rpm\)\s*:\s*(\d+)', value: "int(groups[1])" }
  jsonpath:
    - { key: channel.gain, each: $.channels[*], value: "node.gain",
        labels: { channel: "node.index", name: "node.name" } }
```

The extractor names a `key`. What that key *means* (kind, value type, unit, validation,
fusion) lives on the [`datapoint_type`](/architecture/taxonomy/) registry, not the template:
the template references a registered key and never mints one. Save-time validation rejects any
unregistered key, so a template can never collect a measurement the registry does not know.

## Inputs: the template's typed parameters

A template is a function of its `inputs`: shape-typed parameters it references internally,
never a hardcoded `$var:`. That is the decoupling, a template needs an `ssh_credential`, not
specifically `$var:crestron.ssh`. At `:apply` each input is **bound** to a value, either a
literal or a [variable](/architecture/variables/) reference (`$var:<name>`), with an optional
default the template ships. Required inputs are the apply gate; the UI renders the form.

```yaml
inputs:
  - group: connectivity
    fields:
      - { key: ip,   type: ipv4,           required: true, label: "IPv4 address" }
  - group: auth
    fields:
      - { key: snmp, type: snmp_community, required: true, label: "SNMP community" }
      - { key: ssh,  type: ssh_credential, default: $var:crestron.ssh, label: "SSH login" }
```

The template body references `${input.snmp}` / `${input.ssh}`; the bindings resolve at apply
and are overridable per component. So `$var:` lives at the **binding layer** (apply, and input
defaults), not scattered through the template body, and the template stays reusable with any
value of the right shape. Each input `type` is a `variable_type`, so per-field secrecy comes
from the shape.

## Execution: parse at the edge, raw as a debug sidecar

The flow runs the transform at the **edge**, not server-side:

- **Flow steps parse, extract, and normalize on the node** and emit resolved datapoints
  straight to the typed tables. The compiler bakes each datapoint's `kind` into the runtime
  unit, so the edge writes to `metric_datapoint` versus `state_datapoint` with no runtime
  registry lookup.
- **Raw is still written to `telemetry`** as a TTL'd **debug sidecar**, not the datapoint
  source. Raw-for-debug is retained for a window; replay and backfill after a template change
  are not a guarantee.
- **Single owner (first phase):** datapoints land on the flow's own component, identity stamped
  at the edge (the component is known, the flow runs for it). Fan-out to multiple owners (a
  management platform reporting for many devices) is a later phase.
- Because parsing is the edge step, there is **no separately authored transform rule**. Routing
  is the template's fan-out, and cross-entity rollups are [calc](/architecture/taxonomy/#rules-three-families)
  datapoints on system and location templates. The server-side work that remains is
  shared-interface owner-binding, untemplated raw ingress, and future replay.
