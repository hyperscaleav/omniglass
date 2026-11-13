---
title: "A chart's end dot sits on its line (#1789)"
date: 2026-11-11
refs: [1789, 1786]
---

A bucketed chart drew its line through each bucket's average at the bucket's center, but put its end dot
at the newest bucket's last reading. Wherever the last differed from the average, which for a rising
counter is nearly every minute, the dot floated above or below the end of the line, and the newest
min/max band ran half a bucket past it. Platform health's sparklines showed it on most rows.

The line now runs on from the newest average to the dot, placed at the bucket's end (or now, for the
bucket still filling), so the dot sits on the line and every band stops where the line does.
