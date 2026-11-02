---
title: "The Newtron processor and scheduling panel are simulated and collected (#896)"
date: 2026-11-02
refs: [896, 1266, 1360]
---

The dev seed places Newtron Core 3 control processors and Slate 5 scheduling panels, and both were
listed in `coverage.yaml` with no profile, so the lab collected nothing from them. Each now has a
profile matching what the device is.

The Core 3 speaks the panel's room and source control (`GET INPUT`, `SET INPUT`, the input, scene and
log subscriptions) and its own surface: the program state (`GET PROGRAM`, `SET PROGRAM`, pushed as
`EVT PROGRAM`), the control ports and the count of connected panels. The Slate 5 answers its room's
booking (`GET BOOKING`, `BOOK`, `RELEASE`, `GET NEXT`, pushed as `EVT BOOKING`). Both keep an
`uptime` computed from a boot epoch, and both leave `coverage.yaml`.

The room and source control is defined once. A device interface takes a shared fragment in with
`include:` (`surfaces/nvp-room.yaml`), expanded by the simulator's profile loader, and the Panel 7
now includes it too in place of its own copy; a test holds that the processor answers everything the
panel does. The decision, and the product-catalog primitive it declined, is recorded in ADR-0896.

Two new drivers collect the class signal, both on the Newtron line protocol: `newtron-core-nvp` reads
`video-input` and the new `program-state` property (running or stopped), polled every 30 seconds and
pushed on change, and `newtron-slate-nvp` reads `booking-state`. Each reuses the `newtron-nvp`
poll, listeners and command blocks by YAML anchor rather than restating them. The two products move
to their drivers; `newtron-nvp` and the Panel 7 are untouched. The control ports and the connected
panel count are served but not yet collected: the class's full signal set is #1360.
