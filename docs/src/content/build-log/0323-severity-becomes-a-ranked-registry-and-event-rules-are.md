---
title: "Severity becomes a ranked registry, and event rules are authored (#819, slices #917 and #918)"
date: 2026-10-19
refs: [819, 917, 918]
---

Severity is a registry of named, ranked levels (`severity_level`): five official levels from the boot
seed (info 10 to disaster 50), operator levels added between them, renamed (alarms follow), recolored and
re-ranked at `/severity-levels`. The alarm's three-value CHECK became a by-name reference, added `NOT
VALID` because the levels are boot-seeded after migrations run, and existing alarms mapped by meaning:
`critical` became `high`, the outage floor, so no verdict moved. A component's own verdict reads the
rank. The review caught a re-rank leaving recorded health stale (a re-rank now recomputes every
component with an open alarm at the level) and a future official level able to take over an operator's
(a clash now stops the boot naming it). The console reads the registry through one severity badge. Event
rules are authored at `/event-rules` (gated `rule:*`, fleet-wide config): a registered sample key on its
lane, fire and clear criteria (the placeholder comparison until the expression engine), sustains, a
severity level, and a selector of component types and components; an update bumps the version. The five
rulings are ADR-0144.
