---
title: "The device taxonomy is OAVC, and the demo catalog is fictional"
date: 2026-08-24
status: "Accepted"
summary: "The component_type tree mirrors the OpenAVCloud AV Device Taxonomy v1.1 (category roots, subcategory types, form-factor mic subtypes); the seed catalog goes fictional (the omniglass-lab brand universe, AV-iQ as the realism source); standards become integrator-style room chains with size-serialized variants, and the impossible shared bar yields to the divisible pair's shared DSP and amplifier rack"
---

- **Date:** 2026-08-24 | **Status:** Accepted | **Pages:** [core-entities](/architecture/core-entities/), [storage](/architecture/storage/)
- **Decision:** The `component_type` registry mirrors the OpenAVCloud AV Device Taxonomy
  v1.1: its high-level categories are the tree's roots, its subcategories the types, and
  the concrete classes a standard's slot names the leaves. Microphone subtypes are
  arranged by FORM FACTOR (ceiling, wireless, gooseneck, boundary), never by inner
  technology (beamforming is a capability, not a device kind); dante-card sits under dsp.
  The seed catalog becomes fictional: vendors and SKUs come from the omniglass-lab brand
  universe (Boreal, Kestrel, Lyra, Meridian, Newtron, Polaris, Shinra Cloud, Tessellate),
  with the AV-iQ product database as the shape-realism source, because the demo fleet is
  not sponsored. Standards are written the way an integrator writes room standards: a
  room type's full signal chain, size-serialized where the display drives the variant
  (mr55..mr98 and ds55/ds75 pin the display SKU per variant through
  `parent_standard_id` plus `pinned_products`). A video bar is the ONE all-in-one
  conferencing appliance at the front of a room, never a microphone and never shared;
  the divisible pair's shared component is its DSP and amplifier rack, which replaces
  the physically impossible shared-bar teaching case.
- **Context:** The architect's review of the #796 seed caught video bars filling
  microphone roles across the fleet and a "shared bar" serving two rooms, both
  domain-impossible; the ruling (in chat, 2026-08-24) adopted the OAVC taxonomy for
  types, fictional lab brands for SKUs, and the integrator-style standards catalog,
  recorded here and built as #802.
