---
title: "A disruptive command's hold set follows power and host dependencies (#1648)"
date: 2026-11-09
refs: [1648, 1651, 1652, 1653, 1655]
---

A component now declares what it depends on apart from where it sits: `power` (fed from a named
outlet of a PDU), `host` (hosted by a codec or a chassis) and `path` (reached through a gateway)
edges in one `component_dependency` table, recorded over `/components/{name}/dependencies`, the
generated `omniglass component dependency` commands and MCP
([ADR-1648](/architecture/decisions/#adr-1648-a-disruptive-commands-hold-set-follows-dependency-edges)).
A disruptive delivery's hold set is resolved from them by one pure function: a reboot holds the
target's in-band endpoints and those of what it hosts, transitively, and leaves a BMC marked
`out_of_band` open; a command type naming a `power_outlet_param` holds everything its outlet feeds,
out-of-band included, and never the PDU's own endpoints. Containment holds nothing, and `path` is
recorded for the upstream gate reason (#1652). The resolver's cases are unit tests
(`TestHoldSetRebootFollowsHostChain`, `TestHoldSetPowerFollowsOutlet`,
`TestHoldSetHostCycleTerminatesAndStaysOnNode`); against Postgres a codec's reboot delivery carries
its panel's endpoints (`TestComponentDependencyHostWidensRebootHold`) and an outlet command carries
the fed device's (`TestComponentDependencyPowerCommandHoldsFedComponent`). The example fleet seeds
the boardroom's videobar and amp on the rack power conditioner's outlets 1 and 2 and its touch panel hosted by the
videobar, so `omniglass component dependency list videobar-1` shows both directions on a fresh lab.
No shipped command type is a power command until the outlet command lands (#1653).
