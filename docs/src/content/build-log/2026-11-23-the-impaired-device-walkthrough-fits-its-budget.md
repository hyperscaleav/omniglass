---
title: "The impaired-device walkthrough fits its budget (#1726)"
date: 2026-11-23
refs: [1726, 1647, 884]
---

`watch-an-impaired-device` ran 54.6 to 61.1 s against the 60 s demo budget, so about one run in
three failed `make demo`. The device row's impaired badge, a separate step that usually passed at
once, is folded into the gap step: that step now waits for the snmp gap read as impaired and for
projector-1's badge together, and its caption names both. The walkthrough keeps the whole story
(the gap opens as impaired, then clears after Repair) in ten steps. Six recordings of the new
script ran 49.5 to 54.0 s. The bracket floor (1 s) and the page's 5 s poll stay as they were, so
the real-time waits still set the spread. The docs copy under `docs/public/walkthroughs/` is
re-recorded.
