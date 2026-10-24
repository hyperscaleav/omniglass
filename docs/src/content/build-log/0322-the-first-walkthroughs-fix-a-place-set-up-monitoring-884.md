---
title: "The first walkthroughs: fix a place, set up monitoring (#884, slice 6: #927)"
date: 2026-10-19
refs: [884, 927]
---

Two walkthroughs record under `make walkthroughs` and pass as tests. Find a place and fix it drills
Explore to a place, opens its system, renames the place from the system's Configure tab, and shows the
label follow into the system's header and back on the outline; it is embedded in the
[fleet guide](/guides/operator/fleet/). Set up monitoring attaches the generic SNMP driver and adds an
ICMP endpoint to a simulated display, then reopens its page to show reachability and samples arrive (the
view does not poll yet, #934, so the driver goes first and the probe holds past a node heartbeat); it is embedded in the
[collection guide](/guides/operator/collection/). Node enrollment runs on the node's host, so the
walkthrough uses the node the stack enrolls and says so; the unplug, the rule's alarm and the health verdict
are their own walkthrough, Watch a rule raise an alarm, embedded in the [rules guide](/guides/admin/rules/) (#921). The walkthrough stack adds one simulated-product component with no
endpoint (`bay-2-display`) so the monitoring walkthrough starts from nothing, and the page header carries a
`page-header` test id as a stable wait target. Defects found on the way are filed: #931, #932, #933, #934.
