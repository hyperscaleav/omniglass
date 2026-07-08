---
title: Data collection
description: "Data collection is built from functions: a versioned component template declares interfaces and a set of functions, each a trigger plus a DAG of steps that runs at the edge and parses on the spot."
sidebar:
  badge:
    text: Partial
    variant: note
---

:::note[Partial]
The first collection path is live end to end: an edge node runs real **inline reachability probes** (a **tcp**
connect probe and an **icmp** ping probe) against a component's interface target, ships the result as a protobuf
`Event` over a JetStream durable consumer, and the `tcp.open` / `tcp.connect_time` and `icmp.reachable` /
`icmp.rtt_avg` datapoints land in `metric_datapoint` owned by the target component. The owner is
bound **server-side** from the task's interface (the node stamps no component identity), and the ingest consumer
confines a node to its own tasks (an Event carrying another node's `task_id` is orphan-dropped) and rejects
unregistered datapoint names. The icmp probe rides the same pipeline unchanged (the consumer does not branch on
probe type); a target that does not answer is DATA (`icmp.reachable=0` with a reason), and an error is reserved
for a node that cannot do ICMP at all, told apart by a once-cached loopback capability self-check. On top of the
raw probe metrics the node now computes the per-interface **reachability verdict** `interface.reachable` (up/down,
the AND of that interface's probe results) and emits it as a built-in **state** datapoint; the ingest consumer
**routes by the registry kind** (metric to `metric_datapoint`, state to `state_datapoint`) under the same
confinement, and the state series is **transition-only** (one row per flip, guarded both at the node and at
ingest). The full function/DAG authoring model below (multi-step, cross-interface, branching) is still design;
only the inline tcp and icmp probes are built. See
[ADR-0017](/architecture/decisions/#adr-0017-telemetry-is-a-protobuf-event-over-jetstream-with-an-inline-owner-confining-consumer)
and [ADR-0018](/architecture/decisions/#adr-0018-the-reachability-verdict-is-a-built-in-state).
:::

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
  routes to the right table with no runtime registry lookup, the kind riding the published
  datapoint. The runtime unit is internal, never hand-authored.
- **Edge-local execution.** A function runs per component on the node, in one tick, with zero
  server round trips: every interface sits on the one component, all reachable from the node,
  so a step can branch on a value a prior step just collected, straight from node memory.
- **Two data planes, split by access pattern.** Timeseries [datapoints](/architecture/datapoints/)
  (observed and calculated) are append-heavy and history-bearing. Current-value config and
  credentials live in the separate [config and credentials](/architecture/variables/) store (sargable
  point-lookups); config is keyed to a datapoint as its observed side.
- **Kubernetes-style versioning.** `apiVersion: collection.omniglass.dev/v1alpha1` plus a
  `kind` (`ComponentTemplate`, with `SystemTemplate` / `LocationTemplate` reserved in the same
  apiVersion). The parser gates on `apiVersion` and converts older versions forward.

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
    type: snmp                     # interface_type registry entry (param schema)
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

- **Type is an `interface_type` registry entry**: the registry knows which protocol adapters exist
  and carries each one's connection-param schema. It covers `snmp`, `http`, `ssh`, `telnet`, `tcp`,
  `icmp`, `webhook`, `mqtt`, `syslog`, and `websocket`. The per-type schema is registry-driven, so
  config lints against exactly the adapter the registry holds.
- **`liveness`** is the per-interface reachability gate; it decides whether the interface's
  functions run. See [nodes](/architecture/nodes/).
- **`persistent: true`** keeps a session open across function runs (interface lifecycle contains
  a function run, which contains a step). Scheduled functions borrow it to send; listen functions
  wake on its inbound.
- **Codec and framing.** Raw-TCP AV control planes wrap payloads non-trivially (line
  terminators, length prefixes, NUL framing, JSON-RPC or TTP envelopes). An interface carries
  encode/decode controls that lock raw to shape: the codec frames outbound payloads and parses
  inbound ones to the declared envelope, so a step sees structured content, not wire bytes.
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
A function has exactly one trigger. `triggers` is modeled as a list to admit the multi-trigger
case, a scheduled function that is also command-invocable for a targeted refetch.

### Two axes: task mode and interface transport

A **task** is a node's unit of collection work. Two independent axes describe it, and keeping them
separate is what keeps the model clean.

- **Task mode** (a property of the task): **poll** (we ask for each datum) or **listen** (we wait
  for it to arrive). Stated from *our* perspective on purpose: "pull/push" inverts depending on
  whose frame you take, because the component pushes exactly when we pull. `poll` and `listen` are
  verbs *we* perform.
- **Transport** (a property of the interface): **stateless** (a throwaway connection per shot) or
  **stateful** (a held-open connection, which becomes a `session` and emits `session_log` rows for
  connect/auth/drop/reconnect).

These are orthogonal. All four cells are real:

| | **poll** (we ask) | **listen** (we wait) |
|---|---|---|
| **stateless** | SNMP get, HTTP GET | webhook, SNMP trap, syslog |
| **stateful** | SSH-exec or xAPI `xStatus` on a held session | MQTT subscribe, xAPI feedback |

Waiting for a frame is a single mode (**listen**) regardless of transport; a held-open connection
is a property of the interface, not a separate mode. So there are two task modes, and statefulness
lives on the interface.

**Native push.** First-class data pushed by smart senders (control-system programmers instrumenting
directly) is self-describing (it carries its key), so its edge parse is a near-identity
pass-through, marked `shape=native`. As with any function, a failed parse keeps the raw on a
`collection.failed` event.

## Built interface types and their config

The poll types and listeners in the `interface_type` registry and the operator config they read.
The node translates each stored task + interface into a poller the collection engine runs; how the
node *executes* these (tick scheduling, reachability gating, the task queue) is [nodes](/architecture/nodes/).

### Built poll protocols and their config

| interface type | shape | host/target | per-task params | datapoints |
|---|---|---|---|---|
| `icmp` | inline probe | `task.params.target` | `count`, `timeout` | `icmp.reachable`, `icmp.rtt_avg` (fixed) |
| `tcp` | inline probe | `task.params.target` (`host:port`) | `timeout` | `tcp.open`, `tcp.connect_time` (fixed) |
| `snmp` | held connection | `interface.endpoint` (`host[:port]`, port defaults 161) | `task.params.oids` (comma-separated `name=oid`); `interface.params.version` (default `2c`), `interface.params.community` | one datapoint per OID, `name` = the datapoint key |
| `http` | held connection | `interface.endpoint` (base URL) | `task.params.path` (joined onto the base URL), `method` (default `GET`), `timeout` (default `5s`), `body`, `extract` (comma-separated `name=json:<dot.path>`); `interface.params.header_*` (request headers, prefix stripped) | `http.reachable`, `http.status_code`, `http.response_time` (fixed) + one per `extract` entry |
| `raw-tcp` | held connection | `interface.endpoint` (`host:port`) | `task.params.command` (sent verbatim + line ending), `timeout`, `extract` (comma-separated `name=re:<pattern>`); `interface.params.line_ending` (default `\r\n`), `read_delim` (default `\n`), `connect_timeout`, `read_timeout` | `rawtcp.reachable`, `rawtcp.response_time` (fixed) + one per `extract` entry |
| `telnet` | held connection | `interface.endpoint` (`host:port`) | as `raw-tcp`, plus `interface.params.username`/`password` (drive the default `login:` / `Password:` chain; `login_expect`/`password_expect` override the prompts) | `telnet.reachable`, `telnet.response_time` (fixed) + one per `extract` entry |
| `ssh` | held connection | `interface.endpoint` (`host:port`) | as `raw-tcp` (the command runs as a one-shot `exec`), plus `interface.params.username` and `password` and/or `private_key` (inline PEM) | `ssh.reachable`, `ssh.response_time` (fixed) + one per `extract` entry |

`icmp`/`tcp` are inline probes (the target rides the task); `snmp`, `http`, and
the text transports (`raw-tcp`/`telnet`/`ssh`) are held connections, so the
connection (host/port/version/community for snmp, base URL + headers for http,
address + framing + auth for the text family) lives on the interface and the task
names what to read.

Every fixed built-in name (`icmp.reachable`/`icmp.rtt_avg`, `tcp.open`/
`tcp.connect_time`, `udp.open`, `snmp.reachable`, `http.reachable`/
`http.status_code`/`http.response_time`, and `<proto>.reachable`/`<proto>.response_time`
for the text family) is a **registered canonical `datapoint_type`** in the ship-with registry,
so probe/liveness results persist as datapoints, not only as raw wire
bytes. They are owner-agnostic measurements like any other: unregistered,
reject-not-project would drop them at ingest. `registry.seed_validation_test`'s
`liveness_builtins_present` locks the registry to exactly the names the node
emits, so a rename on either side fails the build instead of silently going
un-derived.

For `snmp`, each OID is carried in its **native SNMP type**: numeric OIDs as
numbers, string OIDs (OctetString / IPAddress / OID) as text, so a string-valued
OID (an enum or label) lands as a `state` datapoint and a numeric one as
`metric`. The owning table is decided at ingest from the key's `datapoint_type`
kind. Per-OID declared typing and richer collection specs live on the component
template (the template declares the OID set, demoting `task.params.oids` to an
override). SNMP runs v2c with a plaintext community or v3 with auth/priv; the
community resolves from the interface params directly or through an `auth_secret`
credential.

Every extract spec (`oids`, the http/text `extract`) shares one name grammar: a
name may carry a trailing **`key[instance]`** suffix to distinguish several values
of the *same* canonical key on one owner (`fan.speed[intake]=<oid>`,
`fan.speed[exhaust]=<oid2>`). The bracket is stripped into the datapoint's
reserved `instance` label, so the canonical registry still matches the bare key
and the value lands in the `instance` column ([the instance dimension](/architecture/datapoints/#the-instance-dimension-many-values-of-one-key-on-one-owner)). A name without a bracket is a singleton (`instance = ''`).

For `http`, `http.reachable` is `1` whenever the request completes a round trip
(`0` on a transport failure: DNS, refused, timeout, TLS), and `http.status_code`
carries the HTTP status separately, so reachability and a `>= 500` status are
distinct alarm signals (a non-2xx response is still reachable). `extract` pulls
values from a JSON body by dot-path (`name=json:data.0.temp`): a number or bool
leaf becomes a `metric`, a string leaf a `state`; a missing path, a
container/`null` leaf, or an unreachable endpoint yields no datapoint. Auth rides
as `header_*` interface params (e.g. `header_authorization: Bearer ...`), resolved
from a plaintext param or an `auth_secret` credential. Carry auth in
`header_*`, never in the URL or body: the request `body` param is **not** stamped
as a datapoint label, and the `target` label is the request URL with its query
string (and any userinfo) stripped, so a token placed in the path query does not
leak into attributes (but is still a bad idea). `method`/`body` support POST/PUT.

For the **text family** (`raw-tcp`/`telnet`/`ssh`), the poll is one ephemeral
round trip: connect, optionally authenticate, send `task.params.command` followed
by the line ending, read the reply (to the `read_delim` for raw-tcp/telnet, to
EOF for ssh's `exec`, bounded by `read_timeout`), extract, close. `<proto>.reachable`
is `1` once the transport opened and the command round-tripped (`0` on a transport
failure: refused, timeout, or rejected credentials, which are connection health, not
errors), and `<proto>.response_time` is absent when unreachable. `extract` pulls
values by **regex named capture** (`name=re:<pattern>`, parallel to http's
`json:`): each named group routes to the datapoint of the same name, or to the lone
datapoint when the pattern has exactly one group; a captured value that parses as a
number becomes a `metric`, otherwise a `state`; a non-matching pattern (or an
unreachable endpoint) yields no datapoint, while a pattern that fails to compile is
a configuration error. Auth resolves from interface params (telnet/ssh `password`,
ssh inline `private_key`) or an `auth_secret` credential, the same posture as snmp's
community and http's `header_*`, and ssh pins the host key. Credentials live on the
interface and are never labelled; the `target` label is the command. The transport is
swappable behind one boundary, so a `raw-udp` request/response poll (datagram in,
reply out) slots in as a fourth kind without new machinery; UDP **listen**
(unsolicited inbound: syslog, snmp-trap) is a different shape and belongs to the
listener runtime. A held session (the stateful transport) carries the same text
family over a persistent connection, with multi-line prompt-expect beyond the first
delimiter, command echo handling, and Q-SYS-style frame/checksum framing; ssh runs
its commands as a one-shot `exec`.

### Built listeners and their config

A **listener** is inbound: rather than us polling, **we wait for pushed data**
(`mode: listen`). That data can arrive several ways, a webhook POST, an
MQTT/subscribe stream, an SNMP trap or syslog line, or a line on a held stateful
session; a webhook is one transport, not the definition. A `webhook` listener is
**server-hosted**: `placement: central` makes the server the
endpoint for inbound external webhooks, so a webhook listen-task is **server-executed
and unassigned** (`node_name IS NULL`); the server's `POST /webhooks/{path}` route is
its runtime, not a node tick.

| field | where | meaning |
|---|---|---|
| `path` | `interface.params.path` | the opaque, unguessable token in the inbound URL (`/webhooks/{path}`); a bearer locator, not the interface name |
| `secret` | `interface.params.secret` | shared secret the sender presents in the `X-Omniglass-Token` header (or `?token=`), constant-time compared |
| `component` | `interface.component` | when set, datapoints pre-bind to that component (trivial owner); when empty, shared-interface ingress is owner-bound server-side by labels |
| `extract` | `task.params.extract` | comma-separated `name=json:dot.path`; number/bool -> metric, string -> state (same extractor as the http poller) |
| `raw_log` | `task.params.raw_log` | optional key to store the whole raw frame under (as JSON when the body parses, else text), the holding-pen an event_rule can later promote |

One or more `mode: listen` tasks bind to a webhook interface; each inbound POST
runs every enabled one, parsing its points under that task's id and **publishing**
them to the JetStream [datapoints](/architecture/datapoints/) stream, the same data
lane the node publishes to (so owner attribution resolves server-side, and the rule
engine and calc rollups react from the stream). The ingest is **owner-confined by the
admission consumer** against the interface's **declared owner**, keyed off the trusted
server-set `interface` label (not a payload claim), so a leaked path secret can publish
only under that interface's owner, never an arbitrary one
([identity and access](/architecture/identity-access/)).

**Response contract** (webhook senders retry on non-2xx): **202** = durably
accepted; **401** bad/absent secret, **404** unknown path, **413** body over the
1 MiB cap (4xx = sender fault, don't retry); **5xx** = our fault, please retry. A
`GET`/`HEAD` to the path answers the endpoint-verification ping some providers
send, echoing a `?challenge=` value. The body cap, JSON-only parsing, and
"non-JSON body makes declared extractions absent (not an error)" mirror the http
poller.

**Auth and spoofing**: the shared secret resolves from a plaintext `interface.params`
value or an `auth_secret` credential (same posture as snmp's community), and the
sender may instead present an HMAC signature verified behind the auth seam. The route
stamps a trusted, server-set `interface` label on every datapoint and copies body
fields into attributes **only** via the declared `extract` set, so a body field cannot
impersonate another interface; shared-interface ingress should scope on
`event.labels.interface`, and per-component interfaces (server-assigned owner)
are preferred for high-trust sources. A listener also runs node-hosted for
LAN-local sources, with idempotency/dedup and form-encoded bodies.

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
  one entry per adapter.

In-scope reference namespaces within a function run: `$var:<key>` (config and secret values,
resolved through the [cascade](/architecture/cascade/)), `$dp.<key>` (datapoints), `$steps.<id>.*`
(ephemeral scratch), `$event` (the inbound payload of a `listen` function), and the
extractor-local inputs a step prepares for its `value` leaf (`raw`, `groups`, `node`, `item`).

### Extractors: locate, then optionally transform

Each extractor is a typed section that locates a raw value with its protocol-specific field,
then optionally transforms it with a single [Expr](/architecture/expressions/) expression in
`value` (default identity).

**One interpolation convention.** Wherever a config, label, or template field could hold either a
computed value or a fixed one, an **interpolated** value (an expression evaluated against the
in-scope context) is wrapped `${...}`, and a **literal** is a bare string. So `${node.index}` reads
the current element's index, while `"main-display"` is the literal text. The `value` leaf is always
an Expr expression by definition, so it needs no wrapper.

```yaml
datapoints:
  oid:
    - { key: device.uptime, oid: 1.3.6.1.2.1.1.3.0, value: "raw / 100.0" }  # centiseconds to seconds
  regex:
    - { key: fan.speed, match: 'fan \(rpm\)\s*:\s*(\d+)', value: "int(groups[1])" }
  jsonpath:
    - { key: channel.gain, each: $.channels[*], value: "node.gain",
        labels: { channel: ${node.index}, name: ${node.name}, role: "main-display" } }
```

The extractor names a `key`. What that key *means* (kind, value type, unit, validation,
fusion) lives on the [`datapoint_type`](/architecture/datapoints/#the-datapoint_type-registry) registry at some
[scope](/architecture/datapoints/#key-scope-template-org-official): a template declares its own keys at
**template** scope (no registry friction), or references an **org** / **official** key. Compile-time
validation resolves every key to a reachable scope (template keys self-resolve; referenced org/official
keys must exist); an unresolved key is reject-not-project at ingest, so a template never silently
collects a measurement no scope knows.

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

- **Function steps parse, extract, and normalize on the node** and **publish** resolved
  datapoints to the JetStream [datapoints](/architecture/datapoints/) stream (the data lane),
  not to the typed tables directly. The node is a NATS client publishing observed datapoints;
  a [persistence consumer](/architecture/datapoints/) batch-writes them to the typed tables as
  an async sink, idempotent on `(series, ts)`, while the [rule engine](/architecture/alarms-actions/)
  consumes the same stream live. The compiler still bakes each datapoint's `kind` into the
  runtime unit, so the routing to `metric_datapoint` versus `state_datapoint` is decided at the
  edge with no runtime registry lookup, and rides on the published message.
- **Raw payloads are not stored**, the datapoint is the source: a dev raw-mode taps the wire bytes
  live while developing, and a parse or validation failure emits a `collection.failed` event
  carrying the raw. There is no telemetry table.
- **Owner attribution:** a single-owner function lands its datapoints on its own component,
  identity stamped at the edge (the component is known, the function runs for it). A function
  that reports for many devices (a management platform) publishes datapoints for multiple owners,
  resolved server-side from the emitted identity labels (below).
- **Placement-scoped writes.** A node publishes only the owners in its **placement visible_set**
  (the owners of the tasks assigned to it). That visible_set expresses as **NATS subject
  permissions** on the node's account, the `node` gateway mode in
  [identity and access](/architecture/identity-access/). At ingest, an emitted owner label
  **outside** that visible_set is **never an authoritative write**: it is treated as an
  **orphan / discovery candidate** and feeds the `discovery_rule` stream (below), so a
  compromised node cannot manufacture writes for owners it was never placed on. The
  perspectives / `disagree` model is the backstop for the other case, a legitimately-placed but
  compromised node reporting bad values for owners it **does** cover; bounding the visible_set
  and corroborating across perspectives are complementary, not the same defense.
- Because parsing is the edge step, there is **no separately authored transform rule**. Routing
  is the template's fan-out, and cross-entity rollups are [calc](/architecture/calculations/)
  datapoints on system and location templates. The server-side work that remains is
  shared-interface owner-binding and untemplated raw ingress.

### Raw sampling: an opt-in re-parsable window

The default is that raw payloads are not retained. An opt-in **`raw_sample`** policy keeps a
bounded window of raw frames so a corrected extractor can re-derive its datapoints over that
window, without reintroducing a telemetry table.

`raw_sample` is **cascade-resolved**, settable on an interface, a task, or a template, and
resolves to one of three values:

- **`off`** (default): no raw retained.
- **`all`**: every frame the matched task collects is buffered.
- **`1-in-N`**: one frame in every N is buffered (sampled), bounding volume on a high-cadence
  source.

The kept frames carry the **immutable function version** that parsed them, so the buffer is
**re-parsable against that exact version**: a corrected extractor re-runs over the retained
window and re-derives the datapoints, retroactively correcting them. The residual is stated
honestly. **Outside** the kept window a wrong-but-conforming parse (one that produced a valid
datapoint from a misread frame) is **forward-fixable only**: the fix applies to new collection,
the already-parsed history is not retroactively corrected because the raw is gone.

The buffer preserves the no-telemetry-table economics: off by default, bounded, sampled, and
short-lived. It is a short-TTL holding pen, range-partitioned and cold-tierable like the
[metric](/architecture/datapoints/) partitions and the [storage](/architecture/storage/) layout
describe, not a parallel history of record.

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
- **Unmatched identities are orphans**, a discovery candidate. The `discovery_rule` is the
  onboarding win: point it at the API and it auto-creates the entities and sets their identity, so you
  never hand-map.

**Best practice.** Map SaaS / cloud telemetry to **system-owned datapoints**, and **wire it into
system health** with a system-scoped `event_rule`. Treat the vendor's own status as an **input to that
judgment, not the verdict**: a UCC platform reporting "offline" is one source's opinion, so corroborate
it (against the codec, occupancy) before downing the room. See [health](/architecture/health/).

### Identity binding: the value-to-owner index

A multiplexed source emits a row tagged with an external identity (a Zoom Room ID, a controller's
slot number); binding that row to an Omniglass owner is a lookup against a **value-to-owner index**.
The index is an **identity arc** on identity config: a `(datapoint_type, value) -> owner` mapping,
where `datapoint_type` is the **match key** (the canonical identity key, e.g. `zoom.room_id`) and
`value` is the external identity the source emitted. The index resolves **in the cascade scope** the
identity config is set at, so an identity declared at a system or location scope binds the rows of
every member below it.

Two sides can supply the match value, and **precedence** is explicit:

- A **declared identity config value** (an identity the operator set on the target) **wins**.
- It falls back to the **observed identity datapoint** that shares the same key (a value the device
  itself reported under that `datapoint_type`).

So ownership resolution reads the **resolved identity** for the key (declared over observed), matches
the emitted `(datapoint_type, value)` against the index, and binds the row to the owner the index
names. The [datapoints](/architecture/datapoints/) ownership-resolution machinery reads this same
index.

### discovery_rule: orphans become candidates

A `discovery_rule` turns the **orphan / unmatched stream** into proposed entities. Its **input** is
every emitted identity that the value-to-owner index does **not** resolve: an unmatched
`(datapoint_type, value)` from a shared-API batch, plus the **out-of-placement labels** a node emits
for owners outside its placement visible_set (above). Pointing a `discovery_rule` at a source is the
onboarding win: it auto-creates the entities and sets their identity, so you never hand-map.

- **What it creates.** Candidate components or owners, each seeded with the identity that surfaced it
  (the `(datapoint_type, value)` becomes the new entity's identity arc), so the next batch from the
  same source resolves through the index instead of orphaning.
- **Idempotent on re-discovery.** Re-seeing an identity the rule already materialized does **not**
  create a duplicate: the rule keys on the `(datapoint_type, value)` it already bound, so a steady
  stream of the same orphan resolves to one candidate.
- **Scope and standing.** A `discovery_rule` carries a cascade **scope** and an `official` / private
  standing like the other rule families (`event_rule`, calc), so a ship-with `official` rule and an
  operator's private rule compose without colliding.

## Storage

The connection registry, the declared connections, and the node's units of work; the physical layout lives on [storage](/architecture/storage/).

| Table | Key columns | Notes |
|---|---|---|
| `interface_type` | name, **built**, direction (in/out), param_schema (jsonb) | the protocol-and-style registry (`ssh`, `http`, `snmp`, `mqtt`, `webhook`, ...); generates the template config schema |
| `interface` | name (per component), interface_type, **component** (nullable: set = pre-bound, null = shared/match-key), params (jsonb), **node** (server-assigned placement) | the connection, declared once ([nodes](/architecture/nodes/)) |
| `task` | **id = content hash**, interface, **mode (poll/listen)**, spec (jsonb), enabled | a node's unit of collection work; dedupes identical work. Parsing to datapoints is the **edge function**, not the task's job |
