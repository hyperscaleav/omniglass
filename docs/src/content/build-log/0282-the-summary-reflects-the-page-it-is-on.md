---
title: "The summary reflects the page it is on"
date: 2026-08-26
refs: [795]
form: bullet
---

- **The summary reflects the page it is on** (#795 review). The rail stops repeating the
  fleet's numbers on every zoom: each scope builds its own tile spec (the fleet over
  systems; a location over its subtree with a children count; a system over its own
  components with slots, active alarms, and shared members; the leaf over itself), and
  `FleetShell` renders the spec without knowing the scope. A summary identical on every
  page was chrome, not information.
