---
title: "A redelivered command's arc reads in time order (#1676)"
date: 2026-11-23
refs: [1676, 1626]
---

The Delivered step of a command's arc stamped the last delivery, which a redelivery pushes past
the settlement, so the arc read as if the command failed before it was delivered. The step now
anchors on the first delivery and its note names the last ("delivered 2 times, last +47s").
