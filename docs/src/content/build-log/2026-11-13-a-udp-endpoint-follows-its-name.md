---
title: "A udp endpoint follows its DNS name (#1104)"
date: 2026-11-13
refs: [1104, 1037, 1103]
---

A udp endpoint whose `host` is a DNS name resolved it once, when its socket bound, so a name that
moved left the endpoint sending to the old address and dropping the new one's replies as foreign
until the node restarted. The request rung now re-resolves the target once per tick and, when the
address changed, moves the socket's peer in place and self-logs the move; the socket stays bound
(it is unconnected), so the connection key is unchanged. A failed resolve keeps the last address, and
a literal IP target never resolves. Per-tick is the cadence; DNS TTLs are not honoured.
