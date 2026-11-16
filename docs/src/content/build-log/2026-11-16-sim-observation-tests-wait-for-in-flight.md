---
title: "The sim's observation tests wait for a request to leave in flight (#1732)"
date: 2026-11-16
refs: [1732]
---

A simulated device settles a request by counting its outcome before it lowers the in-flight count,
the same order the collector's request tally uses, so a read between the two sees the request in
both. The closed loop's unsettled residual (received minus settled minus in flight) reads that
overlap as masked, never as lost, so the order stays and is now commented on `obsToken.end`. The
observation tests waited only for the request to settle and then asserted nothing in flight, which
failed `TestObservedHTTPClientHangUpIsDropped` on main now and then; they now wait for both.
