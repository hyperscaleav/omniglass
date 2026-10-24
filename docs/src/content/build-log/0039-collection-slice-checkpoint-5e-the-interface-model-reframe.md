---
title: "Collection slice (checkpoint 5e): the interface model reframe"
date: 2026-07-19
refs: []
form: bullet
---

- **Collection slice (checkpoint 5e): the interface model reframe.** Before its PR, the interface model was
  corrected: an **interface is an API we intend to call** (named by the protocol it speaks, `web` / `qrc`,
  unique per component), **not** a network interface; `interface_type` is the **transport** (the reach gate), not
  the protocol driver. Reachability is the first gate of a ladder (reach, auth, responds, collecting). The built
  transports grew from `tcp`/`icmp` to also cover `ssh` and `http` (all reach by opening the tcp port; icmp
  pings), the Add-check picker separates the transport **type** from the protocol **name**, and the dev seed now
  models a lab **polaris DSP** with two protocol-named APIs on one device (`web` over http, `qrc` over tcp) so
  `make dev` shows the "two APIs on one box" story. The driver layer that turns a device API into a normalized
  menu of datapoints and functions (OIDs, commands, parse) is a separate, later concern, decided in
  [ADR-0035](/architecture/decisions/#adr-0039-an-interface-is-a-device-api-the-interface-type-is-its-transport-not-its-driver);
  this slice ships only the transport / reach tier of it.
