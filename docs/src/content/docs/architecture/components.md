---
title: Components
description: What a device is, expressed as a versioned component template that mirrors a Zabbix template; how collection runs is its functions.
---

Leaf of the [architecture spine](/architecture/). What a device *is*: the component template,
the device shape. How collection *runs* is its [functions](/architecture/collection/); the
node runtime that executes it is [nodes](/architecture/nodes/).

## The component model

| Family | What it is | Examples |
|---|---|---|
| `component_type` | classification | device, app, cloud-api |
| `component_template` | the **device shape**: everything about a class of device | Polaris DSP 16, Cisco Room Kit Pro, Q-SYS Core |
| `component` | a deployed instance | `dsp-boardroom-3` |

A **`component_template` is the direct mirror to a Zabbix template**: it bundles, as one
versioned unit, everything needed to monitor and control a class of device. Where a Zabbix
template ships items, triggers, macros, and tags, ours ships:

- **collection** authored as [functions](/architecture/collection/) (inputs, interfaces,
  functions), below;
- **commands** (command-triggered functions the device supports, e.g. `reboot`, `set-input`),
  detail in [collection](/architecture/collection/);
- **`datapoint_type` references** (kind / unit / validation live on the registry, see
  [datapoints](/architecture/datapoints/#the-datapoint_type-registry); a template references a key, never mints one);
- required **[config](/architecture/variables/)** and defaults, and the **credential shapes**
  it needs (see [cascade](/architecture/cascade/), credentials);
- default **tags**;
- default **alarms / health** (the trigger mirror; the alarm spoke owns the detail).

A template is authored once and **assigned to an existing component**; the node then executes
the result.

## Collection is functions

A template's collection is authored as [functions](/architecture/collection/): `inputs`
(typed parameters), `interfaces` (connections declared once, possibly persistent), and `functions`
(each a trigger plus a DAG of steps that parse at the edge and emit datapoints). A command is a
command-triggered function in the same model. See [collection](/architecture/collection/) for the
full schema; this page covers the rest of the device shape.

## The rest of the shape

- **Config.** The template declares the [config](/architecture/variables/) a
  component *requires* (connection and inventory facts, e.g. `ip-addr`, `serial`) and
  their defaults. Effective values resolve through the cascade ([cascade](/architecture/cascade/)).
- **Credential shapes.** The template declares the *kinds* of credential the device needs
  (`basic_auth`, `snmp_community`, `bearer_token`); these are
  [`variable_type`](/architecture/variables/) shapes, bound to actual secret values at
  assignment (credentials).
- **Tags.** Default org labels seeded onto the component (`category: audio-dsp`).
- **Alarms / health.** Default `event_rule`s the template ships (the Zabbix-trigger mirror: fan
  stalled, sustained high temp), owned in detail by the alarm spoke.
- **Function trigger params are cascade bases.** A function's `interval: 30s` is the floor of the
  cascade, overridable by a location, group, or the instance (the `poll_interval` example in
  [cascade](/architecture/cascade/)), not a hard value.

## Deploy: assign a template to an existing component

Assigning a template to a component materializes its collection in one action: it binds the
template's required [`inputs`](/architecture/collection/#inputs-the-templates-typed-parameters)
(the `:apply` gate, a 422 lists any unmet required fields), writes the supplied inputs as the
component's [config](/architecture/variables/) (declared, audited), resolves the
interfaces, and compiles the functions to the per-node runtime unit at the server-chosen node.
Re-applying converges. The 80% case is one action, as cheap as "add host".

## Open items

- The `args` typing vocabulary for commands (scalar types first, structured args later) and how
  command results beyond `success-when` map to the `action` row fields.
- Whether a template may declare default `event_rule`s inline or only reference them (co-design
  with the alarm spoke).
