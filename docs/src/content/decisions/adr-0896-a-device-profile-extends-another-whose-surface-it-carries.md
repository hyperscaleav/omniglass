---
title: "A device profile extends another whose surface it carries"
date: 2026-10-29
status: Accepted
summary: "A simulator device profile may name a base with extends: it carries the base's whole surface and adds to it. The base's interfaces come first; one the profile names again keeps its protocol, port and setup and gains the profile's routes, commands, subjects and OIDs; behaviors, computed fields and overrides append; defaults deep-merge with the profile's winning. Resolved at load, bases first, a cycle or a missing base refused; a base never changes. The Newtron Core control processor extends the Panel 7, whose surface is a subset of its own"
refs: [896, 1266]
---

- **Date:** 2026-10-29 | **Status:** Accepted | **Pages:** [simulation](/architecture/simulation/#the-catalog)
- **Decision:** A device profile may name another with `extends` and carry its whole surface,
  adding to it: an inherited interface named again keeps the base's protocol, port, auth, framing and
  welcome (a driver's login may read the welcome as ready), may restate its doc, and takes the
  extending profile's routes, commands and subjects, one with an inherited match replacing that one in
  place and a new one following the base's, and its OID map entries over the base's (changing an
  inherited interface's protocol or port is refused: a different face is a face of its own; naming a
  face the base does not have is refused too);
  behaviors, computed fields and overrides are the base's then the profile's; defaults deep-merge,
  the profile's winning; identity, docs and products are the profile's alone. The store resolves it at
  load (`internal/sim/profile/extends.go`), bases first so a chain resolves whole, refusing a missing
  base or a cycle, re-validating the merged profile (its faces and its computed fields), and never
  changes the base.
- **Context:** #896 left open whether the catalog grows a composition primitive. A touch panel's
  surface is a subset of a control processor's, so the Core CP-3 answers every NVP verb, route and
  face the Panel 7 does; copying the panel's 400-line profile would drift the day the panel gains a
  signal. A product variant (`products:`) is the other sharing the catalog has, and it is for one kind
  of component in variants, which a processor and a panel are not, as the alignment test holds.
- **Drivers do not compose yet:** the processor's driver copies the panel driver's functions, and #1441
  tracks the same primitive for driver specs.
- **Not chosen:**
  - **Copying the panel's interfaces into the processor's profile.** It drifts.
  - **YAML anchors.** They do not cross files, and the profiles are one file each.
  - **Listing the processor as a variant of the panel.** A variant is one kind of component, and the
    alignment test refuses a profile that lists another kind.
