---
title: "The simulator catalog carries no fleet or room archetype vocabulary"
date: 2026-11-12
status: "Accepted"
summary: "The embedded catalog's room archetypes and fleet manifests are retired rather than re-expressed as standards plus counts; a simulated fleet is a dev fleet or a scale lab scenario, both built from the platform's standards"
refs: [914, 911, 1553, 1266]
---

- **Date:** 2026-11-12 | **Status:** Accepted | **Pages:** [simulation](/architecture/simulation/)
- **Decision:** The embedded simulator catalog (`internal/sim/profiles`) holds device profiles,
  surfaces and templates, and nothing above a device. The room archetypes (`systems/*.yaml`), the fleet
  manifests (`fleets/*.yaml`), `profile.SystemProfile`, `Store.Systems`, and the manifest parse and
  compile path (`fleet.Manifest`, `ParseManifestFS`, `Compile`) are deleted, with no shim and no
  compatibility loader. `fleet.Enrich` and `fleet.NameGenerator` stay, since the engine and the scale
  lab's resolver use them. A test over the catalog fails while either directory is embedded or a file
  names a role or system type no standard or registered system type has.
- **Context:** #911 made a simulated system a system built to a standard, which left the archetypes
  as the input of the internal fleet compiler alone, in a vocabulary (`video-bar`, `touch-panel`,
  "Standard Conference Room") of their own. The ticket offered re-expressing the manifests as standards
  plus counts, or retiring them. Retiring won because #1553 had since landed scale lab scenario
  manifests (`internal/lab/scenario`) that are exactly standards plus counts, in the platform's own
  vocabulary, so re-expressing the embedded manifests would have been a second format for the same
  idea. The 1,000-component benchmark no longer used the compiler either (it spreads devices over the
  profile store), and the one remaining consumer, the acceptance test, now serves one system of each
  standard the `corporate` scenario places.
