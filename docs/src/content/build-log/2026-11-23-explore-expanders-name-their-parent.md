---
title: "Explore's expanders name their parent (#1773)"
date: 2026-11-23
refs: [1773]
---

On a fleet with several places of the same name (the large scenario holds six Service Depots), every Explore expander read `Expand Service Depot`, so a screen reader or a script could not tell them apart. A nested expander's accessible name now ends with its parent path (`Expand Service Depot in North Campus`); top-level rows and the visible text are unchanged. The two walkthroughs that open the top-level Service Depot match its name exactly.
