---
title: "A system's view refreshes as its members' health moves (#940)"
date: 2026-10-22
refs: [940]
---

A system's view fetched once at mount, so a member going down or recovering was not seen until a reload,
and Watch a rule raise an alarm opened the system with a fresh load for that reason. The view's own reads
now poll at the component view's 5s cadence (#934): the system's health report (its verdict, Why, the
roles' staffing and the since-line's edges), its declared roles, its data section's latest values, and on
Activity the members' alarms, events and logs; the Data table's 24 hour sparklines every 30s.
Each member's health light came off the fleet view's dots, a fleet-sized read, so the system's members
read (`GET /systems/{name}/members`) now carries each member's own `component_verdict`, the same
recorded value a dot reads, and the view polls that instead; the fleet view is still read once, for the
place, the path and who the members are. Activity's incidents were explained by one alarms read per
member, a poll as large as the room, so a new `GET /systems/{name}/alarms` reads every member's alarms at
once (active ones whatever their age, those cleared in the last 30 days, capped at 500), and an Activity
poll is a fixed seven requests whatever the member count. Nothing polled grows with the fleet, and TanStack holds every
interval while the tab is hidden. The ages on the page (since when, how long an alarm has been raised)
move with a one-second clock rather than the mount's pinned now; the history and the charts read a
clock that ticks each minute, so a second's tick redraws only the ages, and never behind the report's last edge (the docs screenshot gate caught an ongoing incident that opened in the current minute vanishing under a floored clock). Proven by a component test that advances the clock and
watches a member's alarm land as the degraded verdict, the alarm in Why and the member's red light,
with one request per read and no fleet view; another that holds the poll while the tab is hidden; a
third that ages the since-line under an unchanged report; a fourth that keeps a seconds-old incident; a fifth that polls Activity for a fifteen-member system and
counts exactly one alarms read and no per-component request; and API and storage tests of the member's verdict and the system's alarms read (members only, the
window, the cap) across a raise and a clear.

Watch a rule raise an alarm now opens the system in the page, from the panel's Systems it serves,
before the alarm opens, and waits there for the alarm to land in Why with no reload. It records in 44 to
50s (three runs: 49.7s, 44.5s, 49.2s). Following the alarm's event to the rule's blade was measured at 57.9s, too near the minute to hold,
so the rules guide says it in words.
