---
title: "Device profiles share a surface through a simulator include, not a catalog primitive"
date: 2026-11-02
status: "Accepted"
summary: "A surface two simulated devices share (the Newtron panel's room and source control, which the control processor also speaks) is defined once as a fragment under surfaces/ and taken in by an interface's include: list, expanded by the simulator's profile loader. The product catalog (seed) gains no composition primitive, and each product keeps its own driver"
refs: [896, 1266, 1360]
---

- **Date:** 2026-11-02 | **Status:** Accepted | **Pages:** [simulation](/architecture/simulation/#the-catalog)
- **Decision:** (#896, agreed by the architect on #896 as option A.)
  1. **Share in the simulator, by `include:`.** A fragment under `internal/sim/profiles/surfaces/` holds subjects and commands. An interface lists fragments in `include:`; `profile.LoadStoreFS` expands them ahead of the interface's own subjects and commands, before the profile is validated. An unknown fragment, two fragments defining one command or subject, or an interface restating a fragment's command or subject is a load error, so a copy cannot drift in beside the original.
  2. **No composition primitive in the product catalog.** What the processor and the panel share is a fact about what the simulated devices answer, not about what a product or driver is. The seed's products stay flat.
  3. **A driver per product where the signal differs.** `newtron-core-nvp` (input, program state) and `newtron-slate-nvp` (booking state) are separate drivers; the Panel 7 and `newtron-nvp` are unchanged. The three share the `newtron-nvp` poll, listener and command blocks by YAML anchor inside `drivers.yaml`.
- **Context:** #896 asked that the Core 3 and Slate 5 be simulated as what they are, the processor speaking the panel's surface and more, without copying the panel's definition.
- **Not chosen:**
  - **A composition or inheritance primitive in the product catalog.** It models a simulator concern in the data every install reads, and nothing outside the simulator consumes it.
  - **Copying the panel's surface into the processor profile.** The two drift.
  - **One driver for all three products.** The Slate would carry an input poll it cannot answer, and the processor's program state would have no home.
