---
title: "A collection cadence resolves from the platform default and the driver's declaration (#1373)"
date: 2026-10-29
refs: [1373, 1443, 1265]
---

How often an install collects is now a setting. Two new settings in the `collection` namespace on
**Admin > Settings** set the platform's defaults:
- `probe_seconds`, how often each endpoint's reachability is probed;
- `poll_seconds`, how often a driver poll that declares no schedule runs.

A driver's declared `schedule.every` still wins for its poll, and a node's own `--probe-every`
still wins for its probes; both are now optional. Both defaults are the 30-second heartbeat a node
runs at, so an install that changes nothing collects as before, and neither can be set below it:
the heartbeat is the granularity a node runs at, so a faster cadence would be a promise no node
keeps. A node refuses a `--probe-every` below its own `--heartbeat` for the same reason.

Raising the probe cadence is the lever on an install's daily rows: probes write most of a lab's
metric rows, as the lab footprint (#1277) measured. Overrides per component, product and node follow
with the cascade's node leaf, as a typed binding (#1443,
[ADR-1373](/architecture/decisions/#adr-1373-a-cadence-resolves-from-the-platform-default-and-the-drivers-declaration-and-never-runs-below-the-heartbeat)).
