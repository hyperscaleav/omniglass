---
title: "A window across the sim's fleet change reads restarted, not loss (#1895)"
date: 2026-11-15
refs: [1895, 1639, 1623]
---

The browser e2e lab read SNMP loss of about 0.12 on a healthy path in some runs: its first window
spanned the simulator's first sync, and a poll sent to a device not yet bound is never received.
Each simulator observation now carries the fleet generation it served and whether a sync was
starting or stopping devices, and the closed-loop reconcile reads a window that spans a change of
either as `restarted`, the state a counter reset already reads (ADR-1895). The lab report names
such a node under its sim rows as restarted or changed fleet, and the perf gate refuses such a
window as it refuses a restart. A real loss on a steady fleet still reads as loss.
