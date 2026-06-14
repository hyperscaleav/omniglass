---
title: Components
description: The declarative half of collection, what a device is and how you talk to it, expressed as component templates that mirror Zabbix templates.
---

Leaf of the
[architecture spine](/architecture/). The declarative half of
collection: what a device *is* and how you talk to it. The node runtime that
*executes* this lives in [nodes](/architecture/nodes/).

## The component model

| Family | What it is | Examples |
|---|---|---|
| `component_type` | classification | device, app, cloud-api |
| `component_template` | the **device shape**: everything about a class of device | Polaris DSP 16, Cisco Room Kit Pro, Q-SYS Core |
| `component` | a deployed instance | `dsp-boardroom-3` |

A **`component_template` is the direct mirror to a Zabbix template**: it bundles, as
one versioned unit, everything needed to monitor and control a class of device.
Where a Zabbix template ships items, triggers, macros, and tags, ours ships:

- **collection** (interfaces + their pollers / listeners), below;
- **commands** (the `run` actions the device supports), below, detail in
  [alarms-actions](/architecture/alarms-actions/);
- **`datapoint_type` declarations** (kind / unit / validation for what it measures);
- required **props** + defaults, and the **credential shapes** it needs (see
  [cascade](/architecture/cascade/), credentials);
- default **tags**;
- default **alarms / health** (the trigger mirror; the alarm spoke owns the detail).

Collection is one face of the shape, not the whole thing. A template is authored
once and **assigned to an existing component**; the node then executes the result.

## Interfaces: top-level, per protocol

A connection is declared under a **top-level key per protocol**, each an **array of
named objects** (never a map with an unpredictable key, never a polymorphic array
with a `type` field). A given protocol is usually one entry, but the array allows
two where it happens (multiple http endpoints on one component):

```yaml
ssh:
  - name: console
    host: ${component.state.ip-addr}
    port: 22
    credentialRef: ${creds["polaris/admin"]}
    liveness: { ... }        # reachability gate (connection interfaces)
    pollers:  [ ... ]        # read: produce datapoints
    listeners: [ ... ]       # read: inbound pushes
    commands:  [ ... ]       # act: produce caused events + action-row status
```

- The top-level key is the **`interface_type`** (a registry entry with a `built`
  flag and its own param schema). **Session vs exec is a type, not a flag**: `ssh` /
  `ssh-exec`, `telnet` / `telnet-exec`, `tcp` / `tcp-exec`; protocols where the
  distinction is meaningless (`https`, `snmp`, `icmp`) have one type. Inbound types
  (`webhook`, `syslog`, `trap`, `udp`) carry `path` + inbound auth instead of
  `endpoint` + `credential`.
- **`node` is not declared here.** Placement is server-assigned from the
  component's location (see [nodes](/architecture/nodes/)).
- **`endpoint` / `credentialRef` interpolate** from the assigned component's props
  (`${component.state.*}`) and the vault (`${creds["path"]}`), so one template serves
  every device of the class.
- An interface carries its **read primitives** (`pollers` / `streams` /
  `listeners`) and **`commands`** (act) **directly**, no `tasks` wrapper, since
  pollers and listeners self-evidently *are* the read work and commands the act
  work. Both run over the one connection. A connection interface also takes a
  **`liveness`** block, the implicit reachability gate (see [nodes](/architecture/nodes/)).

The config schema for these blocks is **generated from the `interface_type` +
extractor registries**, so it lints against exactly what is built and never drifts.

## Read primitives: pollers, streams, listeners

An interface declares three strongly-typed read sections directly, all sharing
the normalizer contract (extract to datapoints), differing only in trigger:

| Section | Direction | Trigger |
|---|---|---|
| `pollers` | outbound | a schedule fires, send a command/request, read the response |
| `streams` | outbound, held | connect, subscribe, receive a stream |
| `listeners` | inbound | data pushed to an endpoint we expose |

A poller pairs **one command with its extraction** (one command, many datapoints);
the node batches an interface's pollers over the one held session. A stream
declares a `subscribe` spec; a listener an optional `match:` selector.

## Extractors: locate, then optionally transform

Each extractor is a typed section that **locates** a raw value with its
protocol-specific field, then optionally **transforms** it with a single Expr
expression in `value`. Default is identity (use the located value), so the common
case stays clean and the transform is there when normalization is needed:

```yaml
datapoints:
  oid:
    - { key: device.uptime, oid: 1.3.6.1.2.1.1.3.0, value: "raw / 100.0" }  # centiseconds -> seconds
    - { key: dsp.cpu_load,  oid: 1.3.6.1.4.1.55540.2.1.0 }                   # raw is fine
  regex:
    - { key: dsp.fan_rpm,   match: 'fan \(rpm\)\s*:\s*(\d+)', value: "int(groups[1])" }
  jsonpath:
    - { key: channel.gain,  each: $.channels[*], value: "node.gain",
        labels: { channel: "node.index", name: "node.name" } }
```

Each extractor **prepares a named input** for the Expr leaf (`raw` for oid, `groups`
for regex, `node` for a jsonpath element); `value:` is the leaf scalar over it. This
is the locked expression strategy one level down: **Expr in a leaf slot over a
prepared input**, no new language. The transform is **load-shaping, so it runs at
the edge** (the node normalizes during extraction; the `telemetry` carries
normalized values with `raw` kept for replay). Keep it to one-raw-to-one-value;
aggregation across datapoints is the central calc layer, a different slot. The set
of extractors is extensible by adding a typed key (`cel`, more) the same way
protocols are.

## Datapoint keys are declared, not implied by the extractor

The extractor names a `key`; what that key *means* (kind, unit, validation,
authoritative_provenance) lives on the **`datapoint_type`**, declared by the template and
upserted into the private registry on assignment (the Zabbix-item-definition mirror):

```yaml
keys:
  - { key: dsp.cpu_load,    kind: metric, unit: percent }
  - { key: dsp.temperature, kind: metric, unit: celsius }
  - { key: channel.mute,    kind: state,  values: [true, false] }
```

The edge emits only `(key, value)`; central derivation looks up the type and routes
to `metric_datapoint` vs `state_datapoint` (no registry lookup at the edge). Kind and
unit are reusable facts about the measurement, not per-extractor noise.

## Commands: act

`commands` sits under the interface beside the read primitives (`pollers` /
`streams` / `listeners`), because a command uses the
same connection. A command produces **no datapoints**; it is a **`run` action**
([alarms-actions](/architecture/alarms-actions/)) with a name, optional typed `args`, a protocol-specific
invocation, and a `success-when` check. Its result is a caused `event` plus delivery
status / output on the `action` row, not a datapoint:

```yaml
http:
  - name: api
    baseUrl: http://${component.state.ip-addr}
    headers: { X-Polaris-Token: ${creds["polaris/api-token"]} }
    commands:
      - name: mute
        args: { index: int, mute: bool }
        request: { method: POST, path: /api/channels/mute,
                   body: { index: "${arg.index}", mute: "${arg.mute}" } }
        success-when: "$.ok == true"
      - name: reboot
        request: { method: POST, path: /api/admin/reboot }
```

Commands are invoked by the action layer (operator-triggered, or reconciled as
desired state, like registering a webhook callback). The node dispatches them over
the interface as serialized work; see [nodes](/architecture/nodes/).

## Binding source is the interface

How the resulting datapoints find their owner depends on the **interface**, not the
task type:

- **per-component interface** (`component` set, or an interface inside a template
  assigned to one component) produces **pre-bound**, the trivial default owner;
- **shared interface** (an mqtt broker, a site-wide webhook) produces **match-key bound**
  at ingest from a field in the payload / topic / path, via a ship-with rule
  (`by-serial`, `by-mac`, ...). One connection feeds many components.

## The rest of the shape

- **Props.** The template declares the props a component *requires* (the connection
  and inventory facts, e.g. `ip-addr`, `serial`) and their defaults. Effective
  values resolve through the cascade ([cascade](/architecture/cascade/)).
- **Credential shapes.** The template declares the *kinds* of credential the device
  needs (`username_password`, `snmp_community`, `header_token`); the operator binds
  actual credentials by vault path at assignment (credentials).
- **Tags.** Default org labels seeded onto the component (`category: audio-dsp`).
- **Alarms / health.** Default `event_rule`s the template ships (the Zabbix-trigger
  mirror: fan stalled, sustained high temp), owned in detail by the alarm spoke.
- **Task params are cascade bases.** `interval: 30s` in a template is the floor
  of the cascade, overridable by a location, group, or the instance (the
  `poll_interval` worked example in [cascade](/architecture/cascade/)), not a hard value.

## Deploy: assign a template to an existing component

The component and its credentials exist already. Assigning a template materializes,
in one action: the interface instances, their task and command instances, the
resolved `${component.state.*}` / `${creds[...]}`, the server-chosen node
placement, the default binding (datapoints land on this component), and the upsert
of the declared `datapoint_type`s into the private registry. The 80% case stays one
action, as cheap as "add host".

**Shipped today (`POST /components/{name}:apply`).** The first materialize slice
implements this for the collection core: a component pins a template version whose
spec declares `forms` (the operator inputs, required fields = the gate), and
`pollers` / `listeners` whose connection fields are Go templates over those inputs.
Apply gates on the required form fields (a 422 lists the unmet ones and materializes
nothing), writes the supplied inputs as **declared state** (provenance `declared`,
audited), resolves the connection templates, and persists one per-component
interface (`{component}-{type}`) plus one task per poller/listener, stamped with the
requested `node`. Re-applying converges (interfaces upsert by name, tasks are
content-addressed). The poller-oid and form-field keys are datapoint_types validated
against the registry at template-save time, so a template can never collect or
declare a measurement the registry does not know. Credential resolution, commands,
and cascade-based node selection are later slices; today `node` is a request field.
See the API reference.

## Worked example: the Polaris DSP 16

```yaml
apiVersion: omniglass/v1
kind: ComponentTemplate
metadata: { name: polaris-dsp-16, componentType: device }
spec:
  props:
    requires: [ip-addr]
    defaults: { model: "DSP 16", manufacturer: Polaris }
  credentials:
    - { ref: polaris/admin,          type: username_password }
    - { ref: polaris/snmp-community, type: snmp_community }
    - { ref: polaris/api-token,      type: header_token }
  tags: { category: audio-dsp, vendor: polaris }

  datapointTypes:
    - { key: device.uptime,       kind: metric, unit: seconds }
    - { key: dsp.cpu_load,        kind: metric, unit: percent }
    - { key: dsp.temperature,     kind: metric, unit: celsius }
    - { key: dsp.fan_rpm,         kind: metric, unit: rpm }
    - { key: dsp.active_channels, kind: metric, unit: count }
    - { key: channel.gain,        kind: metric, unit: db }
    - { key: channel.mute,        kind: state,  values: [true, false] }
    - { key: channel.peak,        kind: metric, unit: db }

  # SNMP: lightweight hardware baseline (oid extractor)
  snmp:
    - name: snmp
      host: ${component.state.ip-addr}
      version: 2c
      community: ${creds["polaris/snmp-community"]}
      liveness: { oid: 1.3.6.1.2.1.1.3.0 }   # reachability gate (default sysUpTime); disable with `liveness: off`
      pollers:
        - interval: 30s
          datapoints:
            oid:
              - { key: device.uptime,      oid: 1.3.6.1.2.1.1.3.0, value: "raw / 100.0" }
              - { key: dsp.cpu_load,        oid: 1.3.6.1.4.1.55540.2.1.0 }
              - { key: dsp.active_channels, oid: 1.3.6.1.4.1.55540.2.2.0 }
              - { key: dsp.temperature,     oid: 1.3.6.1.4.1.55540.2.3.0 }
              - { key: dsp.fan_rpm,         oid: 1.3.6.1.4.1.55540.2.4.0 }

  # HTTP: per-channel detail (jsonpath + array fan-out) and the control surface
  http:
    - name: api
      baseUrl: http://${component.state.ip-addr}
      headers: { X-Polaris-Token: ${creds["polaris/api-token"]} }
      pollers:
        - request: { method: GET, path: /api/channels }
          interval: 60s
          datapoints:
            jsonpath:
              - { key: channel.gain, each: $.channels[*], value: "node.gain",
                  labels: { channel: "node.index", name: "node.name" } }
              - { key: channel.mute, each: $.channels[*], value: "node.mute",
                  labels: { channel: "node.index" } }
              - { key: channel.peak, each: $.channels[*], value: "node.peak_db",
                  labels: { channel: "node.index" } }
      commands:
        - name: mute
          args: { index: int, mute: bool }
          request: { method: POST, path: /api/channels/mute,
                     body: { index: "${arg.index}", mute: "${arg.mute}" } }
          success-when: "$.ok == true"
        - name: gain
          args: { index: int, gain: float }
          request: { method: POST, path: /api/channels/gain,
                     body: { index: "${arg.index}", gain: "${arg.gain}" } }
          success-when: "$.ok == true"
        - name: reboot
          request: { method: POST, path: /api/admin/reboot }

  # SSH: a push stream for instant mute changes, and the CLI control surface
  ssh:
    - name: console
      host: ${component.state.ip-addr}
      port: 22
      credentialRef: ${creds["polaris/admin"]}
      streams:
        # emulator pushes: ! "subject":"status.channels.0.mute" "value":true
        - subscribe:
            - { path: channels.0.mute }
            - { path: channels.1.mute }
          datapoints:
            regex:
              - key: channel.mute
                match: '"subject":"status\.channels\.(\d+)\.mute" "value":(\w+)'
                value: "groups[2] == 'true'"
                labels: { channel: "groups[1]" }
      commands:
        - { name: reboot,        send: "system reboot",        success-when: '\+OK' }
        - { name: factory-reset, send: "system factory-reset", success-when: '\+OK' }
```

This declares the whole device: what it measures (`datapointTypes`), how
(three interfaces, three extractor styles, a stream), what you can do to it
(`commands` under the interfaces), what it needs (`props`, `credentials`), and how
to file it (`tags`). The node turns it into running collection.

## Open items

- The `args` typing vocabulary for commands (scalar types today; structured args
  later) and how command results beyond `success-when` map to the `action` row fields.
- Whether a template may declare default `event_rule`s inline or only reference them
  (co-design with the alarm spoke).
