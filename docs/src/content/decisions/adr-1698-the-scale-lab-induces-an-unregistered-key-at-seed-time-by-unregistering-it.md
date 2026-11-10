---
title: "The scale lab induces an unregistered key at seed time, by unregistering it"
date: 2026-11-10
status: Accepted
summary: "Accepted: the induced refusal is a seed step, not a simulator profile flag. The scale seed registers a key, writes a copy of one device's driver that emits it, attaches the copy, and unregisters the key; the baked task keeps the name, so ingest refuses it refused-unregistered. Off unless lab run --induce-unregistered asks."
refs: [1698, 1641]
---

- **Date:** 2026-11-10 | **Status:** Accepted | **Pages:** [messaging](/architecture/messaging/)
- **Decision:**
  1. **The refusal is induced in the catalog, not the sim.** A sim answers what a driver polls, and
     a driver's emit names are checked against the catalog when the driver is written and again when
     it is attached. So the scale seed registers `lab-induced-unregistered`, writes a copy of the driver of
     the first planned device that has a plain property poll (`lab-induced-<driver>`), with one extra poll that emits the key
     from a plain property the device already answers, attaches the copy to that device, and then
     unregisters the key. The device's derived tasks keep the baked name; nothing rederives a custom
     driver's tasks, so the node collects it and ingest refuses every sample `refused-unregistered`.
  2. **Off unless asked, scale lab only.** `lab run --induce-unregistered` (hidden, beside
     `--host-memory` and `--ready-timeout`) carries the flag to the scale seed; a seeded fleet's lab
     refuses it. The seed's summary names the device and the driver copy.
- **Context:** the delivery table (ADR-1641) counts a refusal as loss, but the sims emit only valid
  keys, so no lab ever showed one. The issue asked for a simulator option.
- **Rejected:** a simulator profile flag (the sim cannot make a node collect a name no driver
  polls); a driver shipped with an unregistered emit (the catalog check refuses it, as it should);
  unregistering a key an official driver emits (every device on that driver would lose it, and the
  boot seed registers it again).
