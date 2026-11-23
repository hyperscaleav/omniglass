---
title: "The outpost worklist shot waits for the outpost-silent alarm (#2291)"
date: 2026-11-23
refs: [2291]
---

`platform-health-outpost-worklist` renders the same outpost table as `platform-health-outposts` but
had no wait for the lab's outpost-silent alarm, so a capture landing before the alarm read a healthy
row tone and pill where one landing after read outage (the 4.4% failures of #2168). It now waits,
after its click and before its hover, for the outage pill, failing with a message naming the alarm.
The tolerance stays zero. The baseline PNGs are regenerated from a CI capture taken after this change.
