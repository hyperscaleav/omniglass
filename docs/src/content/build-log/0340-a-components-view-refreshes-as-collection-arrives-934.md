---
title: "A component's view refreshes as collection arrives (#934)"
date: 2026-10-20
refs: [934]
---

A component's view fetched once at mount, so after adding an endpoint its Collection and Vitals stayed as
they were until the operator left and came back; only its Simulation section polled. The view's own reads
now poll: reachability, vitals and alarms every 5s (the cadence of a rule's detail and the Simulation
section), and the two fleet-wide reads it borrows (the verdict off the fleet view, and the nodes behind its
Collection card, whose heartbeat age would otherwise go stale under fresh samples and read "node offline")
every 10s. Each is one request however many endpoints or series the component has, and TanStack holds an
interval while the tab is hidden. The alarm ages move with the alarms read rather than staying pinned to
the page's open, which would have read negative for an alarm raised after it. A component test advances
the clock and watches a stale endpoint, a missing vital and an absent alarm all arrive, and another holds
the poll while the tab is hidden.

Set up monitoring no longer reopens the page or holds past a node heartbeat: it closes the new endpoint,
turns to Overview and waits there for the samples, and its poster now shows both endpoints collecting
(#933). It records in 51s to 56s. Watch a rule raise an alarm still ends on the open alarm and the
system it moved: its wait is the probe's detection, the sustain and the sweep, not the component's view,
and the replug and the clear would add about 12s to a recording that runs 53s to 56s.
