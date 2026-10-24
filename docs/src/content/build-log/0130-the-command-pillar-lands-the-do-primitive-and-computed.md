---
title: "The command pillar lands: the do primitive and computed settlement ([ADR-0063](/architecture/decisions/#adr-0063-the-telemetry-model-is-typed-registries-over-bare-noun-data-tables), third and final slice)"
date: 2026-07-28
refs: []
form: bullet
---

- **The command pillar lands: the do primitive and computed settlement ([ADR-0063](/architecture/decisions/#adr-0063-the-telemetry-model-is-typed-registries-over-bare-noun-data-tables), third and final slice).**
  Events know, datapoints happen, and now a **command** records what a component was told, the third
  registry (`command_type`) alongside `property_type` and `event_type`. A migration adds `command_type`
  (with a driver-owned `settle_window_seconds` and an optional `target_property_type`) and the `command`
  invocation table over the owner arc. **Issuing** a command is one transaction that composes the whole
  model: it writes the `command`, a **caused** event (`origin=caused`, typed `command.issued`, #395), and,
  for a settleable command, an **intended** value in the property cache (#394) that the observed value
  settles against. **Settlement is computed, never stored:** within the settle window a difference is
  pending, past it a match is settled and a mismatch is failed, the windowed form of the reconciliation
  read's want/told/is. The registry ships full CRUD (`/command-types`, gated `command_type:*`, seeded
  `set_input`/`reboot`, plus a `video.input` state and a `command.issued` event_type), the `POST
  /components/{name}/commands:issue` custom method (gated `command:issue`), a generated CLI and client,
  and a console **Command Types** catalog page mirroring the property and event catalogs. Deferred (the
  actuation half): the outbound push to a driver or node, the protobuf `Command` on the wire, and the
  device acknowledgement. A new [commands](/architecture/commands/) page lands at `Partial`. Part of
  [#396](https://github.com/hyperscaleav/omniglass/issues/396).
