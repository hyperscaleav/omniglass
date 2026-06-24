---
title: Collection
description: "Collection is built from functions: a versioned component template declares interfaces and a set of functions, each a trigger plus a DAG of steps that runs at the edge and parses on the spot."
sidebar:
  badge:
    text: Spec
    variant: caution
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
- **Two data planes, split by access pattern.** Timeseries [datapoints](/architecture/datapoints/)
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

The built poll types and listeners (`interface_type.built = true`) and the operator config they read.
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
kind. Per-OID declared typing and richer collection specs move to the component
template with authorship (the template declares the OID set, demoting
`task.params.oids` to an override).
v2c plaintext community only this slice; SNMPv3 and credential-backed
(`auth_secret`) community resolution are deferred.

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
as plaintext `header_*` interface params this slice (e.g.
`header_authorization: Bearer ...`); `auth_secret`-backed credential resolution
is deferred, the same posture as snmp's plaintext community. Carry auth in
`header_*`, never in the URL or body: the request `body` param is **not** stamped
as a datapoint label, and the `target` label is the request URL with its query
string (and any userinfo) stripped, so a token placed in the path query does not
leak into attributes (but is still a bad idea). `method`/`body` support POST/PUT;
richer extraction (response headers, regex, JSONPath wildcards), object keys that
contain a literal dot (the extract path separator), and an http liveness probe
are deferred.

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
a configuration error. Auth rides plaintext this slice (telnet/ssh `password`,
ssh inline `private_key`), the same posture as snmp's community and http's
`header_*`; `auth_secret`-backed credential resolution and ssh host-key pinning are
deferred. Credentials live on the interface and are never labelled; the `target`
label is the command. The transport is swappable behind one boundary, so a future
`raw-udp` request/response poll (datagram in, reply out) slots in as a fourth kind
without new machinery; UDP **listen** (unsolicited inbound: syslog, snmp-trap) is a
different shape and belongs to the deferred listener runtime. Deferred for the text
family: persistent held sessions, multi-line prompt-expect beyond the first
delimiter, command echo handling, Q-SYS-style frame/checksum framing, and ssh shell
/ pty (`exec` only).

### Built listeners and their config

A **listener** is inbound: rather than us polling, **we wait for pushed data**
(`mode: listen`). That data can arrive several ways, a webhook POST, an
MQTT/subscribe stream, an SNMP trap or syslog line, or a line on a held stateful
session; a webhook is one transport, not the definition. `webhook` is the first
built listener and is **server-hosted**: `placement: central` makes the server the
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
runs every enabled one, ingesting its points under that task's id through the
server-side ingress path (so owner attribution, parsing, event rules, and calc
rollups all apply).

**Response contract** (webhook senders retry on non-2xx): **202** = durably
accepted; **401** bad/absent secret, **404** unknown path, **413** body over the
1 MiB cap (4xx = sender fault, don't retry); **5xx** = our fault, please retry. A
`GET`/`HEAD` to the path answers the endpoint-verification ping some providers
send, echoing a `?challenge=` value. The body cap, JSON-only parsing, and
"non-JSON body makes declared extractions absent (not an error)" mirror the http
poller.

**Auth and spoofing**: the secret is plaintext in `interface.params` this slice
(same posture as snmp's plaintext community); `auth_secret`-backed resolution and
HMAC-signature verification are deferred behind the auth seam. The route stamps a
trusted, server-set `interface` label on every datapoint and copies body fields
into attributes **only** via the declared `extract` set, so a body field cannot
impersonate another interface; shared-interface ingress should scope on
`event.labels.interface`, and per-component interfaces (server-assigned owner)
are preferred for high-trust sources. Node-hosted listeners (LAN-local sources),
idempotency/dedup, and form-encoded bodies are deferred.

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
fusion) lives on the [`datapoint_type`](/architecture/datapoints/#the-datapoint_type-registry) registry, not the template:
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
  is the template's fan-out, and cross-entity rollups are [calc](/architecture/calculations/)
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

## Storage

The connection registry, the declared connections, and the node's units of work; the physical layout lives on [storage](/architecture/storage/).

| Table | Key columns | Notes |
|---|---|---|
| `interface_type` | name, **built**, direction (in/out), param_schema (jsonb) | the protocol-and-style registry (`ssh`, `https`, `snmp`, `mqtt`, `webhook`, ...); generates the template config schema |
| `interface` | name (per component), interface_type, **component** (nullable: set = pre-bound, null = shared/match-key), params (jsonb), **node** (server-assigned placement) | the connection, declared once ([nodes](/architecture/nodes/)) |
| `task` | **id = content hash**, interface, **mode (poll/listen)**, spec (jsonb), enabled | a node's unit of collection work; dedupes identical work. Parsing to datapoints is the **edge function**, not the task's job |
