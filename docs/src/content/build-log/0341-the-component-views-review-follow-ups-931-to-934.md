---
title: "The component view's review follow-ups (#931 to #934)"
date: 2026-10-20
refs: [931, 934]
---

A component's view no longer polls anything that grows with the fleet. Its verdict rides on its alarms
read (`GET /components/{name}/alarms` now carries the component's own `verdict`, the one its active
alarms make, computed the way the health recompute records it), and the node behind each endpoint is
read one by one (`GET /nodes/{name}`, every 10s), where it polled the whole fleet view and the whole node
list for one verdict and one node. The API health test pins the verdict through an alarm's raise and
clear, and the leaf's poll test counts no fleet view and no node list.

Every age on a component's view, and in its Endpoints panel, now reads one ticking clock (a
`createClock` primitive, a second's grain) rather than the time the data last changed: a poll that
returns the same answer changes nothing a query can see, so a node that died would have left its
endpoint reading "collecting, 4 s ago" indefinitely. Tests hold every read in flight, advance the clock,
and watch the card turn to node offline and the pill to stale.

The Collection card and the Vitals hint say component where they said device ("check the component or
the path", "component reports down"), the product's word for the thing collected from.
