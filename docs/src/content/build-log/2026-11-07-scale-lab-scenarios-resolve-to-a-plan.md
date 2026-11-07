---
title: "Scale lab scenarios resolve to a plan (#1553)"
date: 2026-11-07
refs: [1553, 1357, 1554, 914]
---

The first slice of the scale lab: a scenario manifest describes a vertical environment once, and a
size knob turns it into a plan. `internal/lab/scenario` parses a manifest (sites, each site's
buildings and floors, the standards placed per site as ratios, and the catalog product filling each
role of each standard), validates it, and resolves it at N systems. `corporate` is the one manifest
that ships. `omniglass lab run --scenario corporate --systems N --plan` prints the plan and exits;
without `--plan` it says bring-up lands with #1554.

The manifest speaks the platform's vocabulary, so it cannot invent a device. A standard is a
standard template, and its products are planned through the shared system planner
(`internal/systemplan`) the way a system template's are, so a quorum of two takes two and a choice
is answered by one alternate's roles. A product must be in the shipped catalog and served by a
simulator profile. Every problem comes back at once, each with the manifest line that holds it.

N splits across sites by weight, then each site's share across its standards by ratio, both by the
largest remainder method in whole-number arithmetic with ties going to the earlier entry. The plan
always totals N. Each site is within one system of its quota and each standard within one of its
quota at a site, and the tests hold that bound at 100, 1,000 and 3,000. Labels come from the
simulator fleet's name generator, now exported as `fleet.NameGenerator`, seeded per system, so a
plan is deterministic. The shipped catalog is read without a database through new `seed`
accessors.

The simulator's older fleet manifests (`internal/sim/profiles/fleets`, compiled by
`internal/sim/fleet`) describe sites and devices by profile rather than by standard and product.
Once bring-up consumes these plans they could be retired in favor of scenarios. That retirement is
left to #914.
