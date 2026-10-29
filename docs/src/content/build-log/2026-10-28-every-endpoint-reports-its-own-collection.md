---
title: "Every endpoint reports its own collection (#1369)"
date: 2026-10-28
refs: [1369, 1266, 1373]
---

Every endpoint now carries telemetry about its own collection, on by default, once a report window (a
minute, `collection.report_seconds`). A polled endpoint lands its polls' mean time and, when any
failed, how many. A held session or udp socket lands the messages its listeners claimed (zero when
quiet) and, when any, the ones they did not. A webhook endpoint lands the posts the server accepted
and refused.

A device that only pushes, or that reports nothing of its own, now has telemetry beside its
reachability. This is built-in data, which every endpoint gets whatever its driver, as against the
collected data its driver declares; the drivers guide now draws the line. The cost is a few rows per
endpoint per window, held by a footprint gate at 13 statements and two rows for an endpoint that
polls and listens. Making the window a per-endpoint-type default a driver and an operator can override
is #1373.
