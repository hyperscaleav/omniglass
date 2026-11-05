---
title: "A lab-level test proves the telemetry outcome (#1371)"
date: 2026-11-05
refs: [1371, 1266, 1361, 1367, 1357, 1514, 1515, 1033]
---

`make test-lab` now holds the dev lab to #1266's outcome, tier by tier: after the warm-up, every
component carries its full built-in data, at least one collected class signal beyond identity and
`uptime`, and every system a value of its own. A gap fails naming the owner and the tier.

The check is its own package, `internal/labcheck`, a function of a base URL and a client so the scale
lab (#1357) can call it at any size. Its judgment is pure over a snapshot of the lab, proven without a
lab; its read of the API is proven against a fake one. The expected built-in set is generated, not
listed: the transport registry now declares what an endpoint of each transport writes (always, and
once answered), a driver's shape (polls, listeners, session block) adds the collection telemetry of
#1369, and `probekeys` and the writer classes rule the keys.

The first live run shaped the rules. A down endpoint owes no response time, and a telnet endpoint whose
session is up owes none either (the probe yields to the held session, #1033; the transport registry now
declares that). Endpoints the seed plants a fault on are read from `devseed.PlantedFaults` and owe no
built-in key, though they must still exist. A finding names the component by product and location, since
a name is unique only per room.

The known gaps live in `devseed.TierGaps`, each with its issue: the passive gear of #1367 on the built-in
and collected tiers, the generic device (#1514) and the platform component (#1515) on the collected tier,
and every system on the system tier under #1361 (one coarse entry, since the seeded systems carry no
stable key to name). A listed owner that now carries the tier fails as stale.
