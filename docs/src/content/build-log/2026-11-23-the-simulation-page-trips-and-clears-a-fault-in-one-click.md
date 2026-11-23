---
title: "The Simulation page trips and clears a fault in one click (#2462)"
date: 2026-11-23
refs: [2462]
---

Breaking a simulated device from the console took a raw HTTP request builder: pick
`POST /api/display/power`, hand-edit the body to `{"power":"off"}`, Fire. Each simulated component now has
a **Break** menu on its row and a **Faults** section in its drawer, generated from its profile: power
off, on and standby, unplug and replug, a quick set for each state path an HTTP route sets from a body
field to a fixed list (`Input: hdmi2`, `Mute`), and **Restore**. The drawer's shared fire box is gone: each
operation of the API surface is a row that opens in place to its request and its answer.

What is broken shows as a chip with its words, its age and a clear, on the row, in the drawer, and on the
component's page beside its health. It reads the simulator's own account: a route's mutation `enum` is
now a `sets` entry on the surface, and the simulator reports each device's settable paths held off their
profile defaults in every batch (`faults` on the fleet and component reads, with `impairment_since`), so a
reload shows them. The age of a held path counts from when the server first heard it, so a server restart
starts it again. A command's mutations are not quick sets yet.

The list filters as you type (bare words search name, location, product and address), pages at fifty rows,
and keeps its filter in `?chips=` beside `?c=`, so a deep link reproduces the view. `ListShell` gained
opt-in `live` and `pageSize`, and `FilterBar` an `onDraft` seam; no other list changes.

The drawer is rebuilt on the shape of the archived lab console's: Faults on top, then Info (with a state
form generated from the profile's quick sets), Impairments, Operations, Behaviors (`behaviors` on the
component read) and Events. Events (#2524) and terminal sessions (#2525) need new simulator capability and
are filed; their tabs say so.

Stacked on #2499 (the component detail body) and the primitive wrappers (`claude/feat-primitive-wrappers`).
