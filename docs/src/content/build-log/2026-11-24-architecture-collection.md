---
title: "The Architecture section gains collection and the edge (#2320)"
date: 2026-11-24
refs: [2320, 2153]
---

Seven Architecture pages leave Being rebuilt, rewritten fresh against the code and badged `Partial`:
Data collection, Transports, Outposts, Self-monitoring, Messaging, Workers and Simulation. Data
collection is split three ways: the per-transport wire behavior moves to a new Transports page, and the
platform's account of itself (the outposts' and the server's own figures) joins the outpost
self-report on a new Self-monitoring page. Every link into a moved section is repointed. Each unbuilt
part sits in a design fence naming the open issue that tracks it, and drifted claims (a transport
registry that listed mqtt and syslog, a live console fenced as unbuilt, missing bus subjects) are
corrected to what ships.
