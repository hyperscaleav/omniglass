---
title: "A kept scale lab survives a stop, for a soak run (#1633)"
date: 2026-11-10
refs: [1633, 1737]
---

`omniglass lab run --scenario corporate --systems N --keep` keeps a scale lab's two volumes when it
stops, the way the learning lab keeps its own, so a service manager can stop it with SIGTERM and
start it again on the same fleet and data. The scale path runs throwaway only without `--keep`, and
passes `--fresh` through. The seed step carries a hidden `--keep`: on a database that holds data it
checks the fleet is the plan's (one node per site, each site's simulated components) and keeps it,
re-minting each node's enrollment token and the lab user's password, rather than seeding again; a
kept lab started at another scenario or size is refused, naming `--fresh`. The stop line and the
ready banner name a kept scale lab's own restart command. `--keep` is refused on a seeded fleet, whose
lab keeps its data already, and beside `--ephemeral`. The lab guide gains
[a soak run](/guides/lab/#a-soak-run): the pinned build, the lab and sampler units, retention inside
the run, reading the series, upgrading mid-soak, and reaching the console through a tunnel. The soak
host and its 72-hour run are not in the repo; worker-run soaks are #1737.
