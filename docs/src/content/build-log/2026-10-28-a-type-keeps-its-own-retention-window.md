---
title: "A type keeps its own retention window (#417)"
date: 2026-10-28
refs: [417, 1265]
---

The retention pass kept every metric sample for the metric lane's window and every property
transition for the property lane's. A busy probe metric and a rare, important property had no way
to differ. Now a metric or property type can carry its own window:
`omniglass metric-type setRetention tcp-connect-time --days 3` keeps that type three days, and
`--days null` returns it to the lane's.

The types that most want a window of their own are the shipped ones, which are otherwise read-only.
So the window is set by a method of its own rather than an edit, it works on an official type, and
the boot seed never resets it: a type's definition is the seed's, how long an install keeps its
samples is the install's. The lane's prune skips an overridden type, and the pass prunes each one
to its own window, with the same floor and the same per-row index search.
