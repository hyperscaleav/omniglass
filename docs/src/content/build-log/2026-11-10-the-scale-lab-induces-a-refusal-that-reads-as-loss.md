---
title: "The scale lab induces a refusal, and its delivery table reads it as loss (#1698)"
date: 2026-11-10
refs: [1698, 1641, 1659, 1692]
---

A scale lab brought up with the hidden `lab run --induce-unregistered` (off unless asked) makes one
simulated device answer a property key no driver registers. The scale seed registers
`lab-induced-unregistered`, writes a copy of the driver of the first planned device that has a plain property poll, whose extra 10 s poll
emits it from a plain property the device already answers, attaches the copy, and unregisters the
key, so the device's tasks keep collecting a name the catalog no longer has and ingest refuses each
sample `refused-unregistered`. The seed's summary names the device and the driver copy. The
labfootprint integration case `TestIntegrationScaleDeliveryInducedUnregistered` brings a lab up with
the knob and asserts the delivery table's property row reads a loss above 0 with that cause while
every metric row reads 0, which proves a refusal is counted as loss rather than out. The lab's
integration tests now take `OMNIGLASS_LAB_IMAGE`, so a run against a private tag never rebuilds the
shared `omniglass:dev`. The other refusals are not induced. The decision is
[ADR-1698](/architecture/decisions/#adr-1698-the-scale-lab-induces-an-unregistered-key-at-seed-time-by-unregistering-it).
