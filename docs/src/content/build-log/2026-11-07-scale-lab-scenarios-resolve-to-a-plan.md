---
title: "Scale lab scenarios resolve to a plan (#1553)"
date: 2026-11-07
refs: [1553, 1357, 1554, 914]
---

The first slice of the scale lab: a scenario manifest describes a vertical environment once, and a
size knob turns it into a plan. `internal/lab/scenario` parses a manifest (site archetypes, each
archetype's buildings and floors, the standards placed at its sites as ratios, and the catalog
product filling each role of each standard), validates it, and resolves it at N systems. `corporate` is the one manifest
that ships. `omniglass lab run --scenario corporate --systems N --plan` prints the plan and exits;
without `--plan` it says bring-up lands with #1554.

The manifest speaks the platform's vocabulary, so it cannot invent a device. A standard is a
standard template, and its products are planned through the shared system planner
(`internal/systemplan`) the way a system template's are, so a quorum of two takes two and a choice
is answered by one alternate's roles. A product must be in the shipped catalog and served by a
simulator profile. Every problem comes back at once, each with the manifest line that holds it.

The number of sites scales with N, so a big plan means many nodes rather than a few big ones. A
manifest declares one headquarters, placed exactly once, plus repeated archetypes, each with a
weight and a typical number of systems per site, and the bounds on its site count. The plan has
the headquarters plus one site for each average repeated site's worth of systems beyond the
headquarters' typical size, clamped to the bounds. The repeated archetypes share the other sites by
weight, and their sites are numbered from the archetype's name (`regional-03`, `branch-12`).
Corporate's bounds of 3 to 30 give 3 sites at 100 systems, 18 at 1,000 and 30 at 3,000, an
enterprise of the size the scale lab targets. A missing or doubled headquarters and inverted bounds
are refused with their line.

N is then split across the sites, each taking one system and the rest going by typical size, and
then each site's share across its standards by ratio. Both splits use the largest remainder method
in whole-number arithmetic, with ties going to the earlier entry. The plan always totals N. Each
site is within one system of its quota and each standard within one of its quota at a site. The
tests hold those bounds at 100, 1,000 and 3,000, and they check that the site count stays inside
the bounds and never falls at any size the knob takes. Labels come from the
simulator fleet's name generator, now exported as `fleet.NameGenerator`, seeded per system, so a
plan is deterministic. The shipped catalog is read without a database through new `seed`
accessors.

The simulator's older fleet manifests (`internal/sim/profiles/fleets`, compiled by
`internal/sim/fleet`) describe sites and devices by profile rather than by standard and product.
Once bring-up consumes these plans they could be retired in favor of scenarios. That retirement is
left to #914.
