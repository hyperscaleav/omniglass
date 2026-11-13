---
title: "Ambiguous name-shaped addresses show a message"
date: 2026-11-12
refs: [853]
---

Fixed #853: a name-shaped component, location, or system address now:
- resolves to the UUID and redirects only when the name matches exactly one entity
- shows "More than one [noun] has this name; open it from Explore" when ambiguous
- shows "No [noun] answers this address..." only when truly missing

Previously, ComponentLeaf would silently redirect to an arbitrary match (first in list order), and LocationZoom would render as if the resource didn't exist.

This implements the rule from #759: names resolve only when unique.
