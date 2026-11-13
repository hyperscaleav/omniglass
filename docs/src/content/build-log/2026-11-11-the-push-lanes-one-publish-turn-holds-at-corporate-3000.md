---
title: "The push lane's one publish turn holds at corporate 3,000 (#1746)"
date: 2026-11-11
refs: [1746, 1696, 1743, 1758, 1623]
---

The corporate scenario ran at 1,000 and 3,000 systems with a booking service and a meeting-platform
webhook account pushing at every site, each measured over a 10 minute window after a 3 minute
warm-up, and the push lane's turn figures read from the lab database. At 1,000 (18 sites, 36
pushers) the window held 1,800 pushes, 3 a second; the per-minute p99 turn wait ran 0.005 to
0.0125 ms, the worst wait 0.035 ms, and no push was refused. At 3,000 (30 sites, 60 pushers) it held
3,001 pushes, 5 a second; the p99 ran 0.004 to 0.0105 ms, the worst wait 2.18 ms in one minute, and
again no push was refused. Against a 5 second bound the turn is almost always free, so the
server-wide publisher stays
([ADR-1746.2](/architecture/decisions/#adr-17462-the-push-lanes-single-publisher-holds-at-the-corporate-3000-push-load)).
Every loss figure read at both sizes was 0, but at 3,000 the delivery read never settled: the
consumer, 74% busy at 50 batches a second, always held one batch, so the node and ingest hops read
`unsettled` (#1758).
