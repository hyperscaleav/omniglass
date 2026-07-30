---
title: Commands
description: "The do primitive: the command_type registry, command invocations, and computed settlement that ties want, told, and is together."
sidebar:
  badge:
    text: Partial
    variant: note
---

:::note[Partial: the record and reconcile half is built; actuation is deferred]
The `command_type` registry, the `command` invocation log, and computed settlement are built
([ADR-0063](/architecture/decisions/#adr-0063-the-telemetry-model-is-typed-registries-over-bare-noun-data-tables)).
Still directional: the outbound **actuation** path (the physical push to a driver or node, the
protobuf `Command` on the wire, the device acknowledgement), and the `event_rule`-driven
`derived` producers. #396 is the do primitive's record-and-reconcile half.
:::

A **command** is the "do" half of the telemetry model, the third of the triad alongside a
[property](/architecture/properties/) (**know**) and an [event](/architecture/events/) (**happen**).
Where a property's samples record what a device reports and an event records what happened, a command
records what a component was **told**. Its registry is `command_type`, the driver-owned catalog of
what a component can be told, the twin of `property_type` and `event_type`.

## The command_type registry

`command_type` describes every command: `(name, display_name, params_schema, settle_window_seconds,
target_property_type_id, official)`. Two facts are the driver's, not the abstract signal's:

- **`settle_window_seconds`** is how long the device physically takes to actuate. The driver knows
  a projector warms up in twenty seconds and a matrix switch flips in one; the settle window is
  that fact, so a difference from the reported value is not called drift until the device has been
  given time to act.
- **`target_property_type_id`** is the uuid FK to the property a **settleable** command sets (`set_input` targets
  `video.input`). A command with no target is **fire-and-forget** (`reboot`): it records the
  invocation and a caused event, with no value to settle.

The registry is seeded official and operator-extensible, official rows read-only, on the same shape
as the [property](/architecture/properties/#the-property_type-registry) and
[event](/architecture/events/#the-event_type-registry) catalogs (a console **Command Types** page,
`/command-types` CRUD gated by `command_type:*`).

## Issuing a command composes the whole model

Issuing a command is one transaction that writes three things, which is where the three pillars
meet:

1. the **`command`** row (the invocation: owner, command_type, params, actor), over the same
   [exclusive owner arc](/architecture/properties/#ownership-the-exclusive-arc) as every sample
   and event;
2. a **caused `event`** ([`origin=caused`](/architecture/events/#events-caught-caused-derived-scheduled),
   typed `command.issued`), the lineage record that a command happened; and
3. for a settleable command, an **`intended`** value in the [property cache](/architecture/properties/)
   (`provenance=intended`, its `ts` the moment of issue), the **told** in the want/told/is pivot.

So a command is not a side channel: it feeds the same cache and event log everything else reads.
`POST /components/{name}/commands:issue` is the write, gated by `command:issue` and scope-injected
through the component.

## Settlement is computed, never stored

A command's effect is judged, not recorded. The verdict is a pure function of the intended value it
opened, the observed value the device reports, and the settle window:

- **none**: nothing was told (a fire-and-forget command, or no intended value).
- **pending**: still within the settle window since the command was issued, so a difference from
  observed is not yet drift, the device is given time to actuate.
- **settled**: past the window, the observed value matches the intended one, the command took effect.
- **failed**: past the window, the observed value does not match (or is absent), the command did not
  take effect.

This closes the loop the [reconciliation read](/architecture/variables/) opens: a command sets
**told**, the device reports **is**, and settlement is whether they agree within the window the
driver declares. It is the windowed form of the same `told` versus `is` comparison, so nothing new
is stored, only judged on read.

## Storage

The physical layout (the owner arc, the caused-event lineage, partitioning) lives on
[storage](/architecture/storage/).

| Table | Key columns | Notes |
|---|---|---|
| `command_type` | name, params_schema (jsonb), **settle_window_seconds**, **target_property_type_id?**, official | the do registry; the settle window and target property are driver facts |
| `command` | id, ts, owner arc, command_type_id, instance, params (jsonb), actor, **caused_event_id** | the invocation log; `caused_event_id` points at the event the command recorded |

Related: [properties](/architecture/properties/) (the intended value a command opens),
[events](/architecture/events/) (the caused event it records), [config, secrets, and
variables](/architecture/variables/) (the reconciliation read settlement closes), and
[alarms and actions](/architecture/alarms-actions/) (where a reconcile policy issues a command).
