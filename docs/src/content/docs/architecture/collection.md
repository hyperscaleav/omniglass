---
title: Collection
description: "Collection is built from functions: a versioned component template declares interfaces and a set of functions, each a trigger plus a DAG of steps that runs at the edge and parses on the spot."
---

Collection is built from **functions**. A versioned `ComponentTemplate` declares how to reach a
class of device (its interfaces) and a set of **functions**, each a discrete unit of device logic
that runs on the edge node, reaches the device over an interface, and parses the answer right
there into datapoints. One authoring schema covers everything from a single SNMP read to a
multi-step, cross-interface, branching procedure.

## Model overview

Three strongly decoupled levels, plus a typed parameter surface and template metadata:

```text
ComponentTemplate (apiVersion, kind, metadata.labels)
  inputs       typed parameters; required = the :apply gate
  interfaces   connections, declared once, may be persistent/stateful, own liveness
  functions    each = one trigger + a DAG of steps
    steps      typed operations (kind), gated by an interface, schema-validated
```

- **Authoring compiles to a runtime unit.** The hand-authored template is the contract. A
  compiler lowers it to the per-node execution unit the node runs: it resolves inputs and
  variables, validates the DAG, and bakes each datapoint's `kind` into the unit so the edge
  writes to the right table with no runtime registry lookup. The runtime unit is internal,
  never hand-authored.
- **Edge-local execution.** A function runs per component on the node, in one tick, with zero
  server round trips: every interface sits on the one component, all reachable from the node,
  so a step can branch on a value a prior step just collected, straight from node memory.
- **Two data planes, split by access pattern.** Timeseries [datapoints](/architecture/taxonomy/)
  (observed and calculated) are append-heavy and history-bearing. Current-value config and
  credentials live in the separate [config and credentials](/architecture/variables/) store (sargable
  point-lookups); config is keyed to a datapoint as its observed side.
- **Kubernetes-style versioning.** `apiVersion: collection.omniglass.dev/v1alpha1` plus a
  `kind` (`ComponentTemplate`, later `SystemTemplate` / `LocationTemplate`). The parser gates
  on `apiVersion` and converts older versions forward.

A **function** is the device-level unit. The platform-level workflow that *responds* to data,
the thing that opens tickets, notifies, and orchestrates, is a [flow](/architecture/alarms-actions/);
a flow can call a function, but the two live at different layers.

## Interfaces: connections, declared once

A top-level `interfaces` array, each a named connection. The connection is **decoupled from the
work**: a function's steps reference an interface by `id`; the interface owns the connection, not
the step. Declaring it once removes per-step duplication, and the decoupling lets a
**persistent session outlive any single function run**, so subscriptions and inbound streams
attach to a connection established once.

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
    persistent: true               # stateful session, outlives function runs
```

- **Type is an `interface_type` registry entry** with a `built` flag (`snmp`, `http`, `ssh`,
  `telnet`, `tcp`, `icmp`, `webhook` built; `mqtt`, `syslog`, `websocket` coming). The per-type
  connection-param schema is registry-driven, so config lints against exactly what is built.
- **`liveness`** is the per-interface reachability gate; it decides whether the interface's
  functions run. See [nodes](/architecture/nodes/).
- **`persistent: true`** keeps a session open across function runs (interface lifecycle contains
  a function run, which contains a step). Scheduled functions borrow it to send; listen functions
  wake on its inbound.
- **Codec and framing.** Raw-TCP AV control planes wrap payloads non-trivially (line
  terminators, length prefixes, NUL framing, JSON-RPC or TTP envelopes). An interface carries
  encode/decode controls to lock raw to shape; basic framing ships first, the richer codec
  layer extends over time.
- **Node placement is not declared here.** It is server-assigned from the component's location.

## Functions: a trigger plus a step DAG

A top-level `functions` array. Each **function** is one trigger and a DAG of steps, from trivial
(one SNMP step reading 20 OIDs) to a multi-step branching procedure. A function is a discrete
unit of device logic: it does one thing to or for a component.

A function's **trigger** is one of three kinds, and the three unify what used to be separate
primitives, a poller, a listener, and a command:

| Kind | Fires when | This is |
|---|---|---|
| `schedule` | an interval elapses, or `onStart` once when the interface comes up | a poll (and `onStart` arms subscriptions) |
| `listen` | inbound data arrives on a `source` (`webhook` / `trap` / `syslog`, or `subscribe` / `stderr` / `session-line` on a persistent interface) | a listener for pushed data |
| `command` | invoked on demand, by an operator or by a [flow](/architecture/alarms-actions/) | an action you run against the device (`reboot`, `set-input`) |

A `command` function takes typed `args` and is the imperative path: it is how the platform *acts*
on a device, and how a reconcile pushes a declared config back (the **set** function, see [config](/architecture/variables/)).
`triggers` is modeled as a list; the first phase enforces exactly one. The foreseeable
multi-trigger case is a scheduled function that is also command-invocable for a targeted refetch.

## Steps: the DAG

A step is a typed **operation**: a `kind` (the operation it performs) that runs on a referenced
`interface` and, for a read, produces datapoints through a typed extractor.

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
  array order is cosmetic, so a function editor can round-trip the graph.
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

In-scope reference namespaces within a function run: `$var:<key>` (config and secret values,
resolved through the [cascade](/architecture/cascade/)), `$dp.<key>` (datapoints), `$steps.<id>.*`
(ephemeral scratch), `$event` (the inbound payload of a `listen` function), and the
extractor-local inputs a step prepares for its `value` leaf (`raw`, `groups`, `node`, `item`).

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

A template takes typed `inputs`: shape-typed parameters it references internally, never a
hardcoded `$var:`. That is the decoupling, a template needs an `ssh_credential`, not specifically
`$var:crestron.ssh`. At `:apply` each input is **bound** to a value, either a literal or a
[variable](/architecture/variables/) reference (`$var:<name>`), with an optional default the
template ships. Required inputs are the apply gate; the UI renders the form.

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

## Execution: parse at the edge

A function runs the parse at the **edge**, not server-side:

- **Function steps parse, extract, and normalize on the node** and emit resolved datapoints
  straight to the typed tables. The compiler bakes each datapoint's `kind` into the runtime
  unit, so the edge writes to `metric_datapoint` versus `state_datapoint` with no runtime
  registry lookup.
- **Raw payloads are not stored**, the datapoint is the source: a dev raw-mode taps the wire bytes
  live while developing, and a parse or validation failure emits a `collection.failed` event
  carrying the raw. There is no telemetry table.
- **Single owner (first phase):** datapoints land on the function's own component, identity
  stamped at the edge (the component is known, the function runs for it). Fan-out to multiple
  owners (a management platform reporting for many devices) is a later phase.
- Because parsing is the edge step, there is **no separately authored transform rule**. Routing
  is the template's fan-out, and cross-entity rollups are [calc](/architecture/taxonomy/#rules-calc-event-action)
  datapoints on system and location templates. The server-side work that remains is
  shared-interface owner-binding and untemplated raw ingress.

## Shared-API collection: one component, many owners

Some sources describe **many entities at once**: a SaaS / UCC platform (Zoom, Teams), a controller
fronting many devices, a building gateway. Modeling each described entity as its own component is the
legacy-platform reflex. Here the API is **one component** (one interface, one credential) and its data
**fans out** to the entities it describes.

- The API component's function pulls the batch (all rooms, all devices) in one call and **labels each
  emitted datapoint with the external identity** it belongs to (a Zoom Room ID).
- The function does **not** stamp the owner, it is the conduit, not the owner. Ownership is **resolved
  server-side**: the identity is matched against a declared **identity config** (`zoom.room_id` on the
  target) and the datapoint is bound to that entity. This is the same shared-ingress owner-binding the
  model uses for webhooks and traps; a pull-side batch is the same shape.
- **The owner can be a system, not only a component.** SaaS state that is telemetry *of* a room (no
  physical device) maps to **system-owned datapoints** directly. Reserve a virtual component for the
  genuine *member* case (its own node in the topology, a `health_role`, a lifecycle). Rule of thumb:
  **member -> component, telemetry -> system.**
- **Unmatched identities are orphans**, a discovery candidate. The deferred `discovery_rule` is the
  onboarding win: point it at the API and it auto-creates the entities and sets their identity, so you
  never hand-map.

**Best practice.** Map SaaS / cloud telemetry to **system-owned datapoints**, and **wire it into
system health** with a system-scoped `event_rule`. Treat the vendor's own status as an **input to that
judgment, not the verdict**: a UCC platform reporting "offline" is one source's opinion, so corroborate
it (against the codec, occupancy) before downing the room. See [health](/architecture/health/).
