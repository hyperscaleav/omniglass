---
title: "The fleet-history shot holds still as the lab ages (#2707, #2802)"
date: 2026-11-27
refs: [2707, 2802]
---

Two captures of the same tree differed in `fleet-history.png` because the shot read the lab's timing: the room's first
second of commissioning is a row in the Activity tab's health history on a slow host and none on a fast one (a stretch
under half a second is dropped), which moved the events and logs below it, and the alarm marker sits at the raise's
share of a window that runs from the lab's first record to now. The capture now takes the non-ongoing rows out of both
renders (a new `hide` step) behind an `expect-count` guard that refuses a list that still shows other than one row,
and masks the marker row whole. No console change. `web/e2e/shotsteps.spec.ts` pins both.
