---
title: "Degraded alarms fit the device class (#1208)"
date: 2026-11-14
refs: [1208]
---

The degraded dev-seed scenario cycled the fixture's alarm templates over every n-th component with no
check that the template fit, so "Lamp hours approaching end of life" landed on microphones, speakers,
DSPs, schedulers and processors. A fixture alarm now declares `applies_to`, a list of component type
ids (a type matches itself and its descendants; empty means any component), and `Degrade` takes the
shipped product-to-component-type map and the type parents, staying pure. The lamp-hours template is
tagged `[projector, display]`; a component no cycled template fits takes the first unrestricted
template, so the density (about every third component) and the dedup keys are unchanged. Tests: a unit
test over a mixed-type document (no lamp alarm off a projector or display, projectors and displays
still receive it, density unchanged) and one over the shipped fleet and catalog.
