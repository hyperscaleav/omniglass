---
title: "A disruptive command's hold set follows dependency edges"
date: 2026-11-09
status: Accepted
summary: "Accepted: a component declares what it depends on apart from where it sits, as power (fed from an outlet), host (hosted by) or path (reached through) edges in one component_dependency table. One pure resolver turns a disruptive delivery's target and those edges into its hold set: a reboot holds the target's in-band endpoints and what it hosts; a power command (its type names a power_outlet_param) holds every endpoint of what the outlet feeds and what those host, never the PDU's own. Out-of-band endpoints ride through a reboot. Containment holds nothing; path is recorded and left to the upstream gate reason."
refs: [1648, 1651, 1652, 1653, 1654, 1655, 1656]
---

- **Date:** 2026-11-09 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/#dependency-edges-what-a-component-needs-apart-from-where-it-sits), [commands](/architecture/commands/#the-hold-set-follows-dependency-edges), [collection](/architecture/collection/#endpoints-the-devices-apis-declared-once), [nodes](/architecture/nodes/#implicit-reachability)
- **Decision:**
  1. **Containment is not dependency.** A component's `parent_id` says where it sits (a line card
     in its chassis, a display in its rack); it says nothing about what goes down with what. A
     display does not reboot because its rack's switch does, and a touch panel that lives in a
     different rack does reboot with the codec that hosts it. Dependency is its own fact, recorded
     explicitly, and a reboot never walks the containment tree.
  2. **One table, three kinds.** `component_dependency` holds an edge from a dependent to its
     upstream with a `kind`: `power` (fed from the upstream's named outlet; the outlet is required
     on this kind and refused on the others), `host` (hosted by the upstream, so a reboot of the
     upstream takes it down) and `path` (reached through the upstream). The schema refuses a self
     edge, an outlet on the wrong kind and a duplicate; the gateway also refuses an edge that would
     close a cycle within its kind. Deleting either end deletes the edge. The edge is a fact about
     the dependent, so a write costs `component:update` on it and only `component:read` on the
     upstream.
  3. **Each kind propagates one thing.** `power` propagates power commands only: the PDU's own
     gate closing (its reboot, its unreachability) holds nothing it feeds, since the outlets stay
     energized. `host` propagates disruption, not unreachability: a codec that reads down does not
     hold its panel, a codec that reboots does. `path` propagates any closed gate, which is the
     `upstream` reason's question (#1652); this slice records it and does not follow it.
  4. **Out-of-band is an endpoint's fact.** `endpoint.out_of_band` marks an API that answers
     through a reboot of its device (a BMC, a management port), so a reboot leaves its gate open.
     A power command still holds it: the outlet takes the whole device dark.
  5. **A power command is marked on its type.** `command_type.power_outlet_param` names the param
     that carries the outlet. It is set only on a disruptive type (a CHECK and a 422). No shipped
     type sets it until the outlet command lands (#1653), so today every disruptive delivery
     resolves as a reboot.
  6. **One pure resolver.** `storage.ResolveHoldSet` takes the target, the outlet (or none), the
     edges and the endpoints, and returns the held endpoint ids; the loader reads the edges and
     endpoints reachable from the target and calls it. The walk keeps a visited set, so a cycle
     that slipped past the create check (two creates racing) terminates and each endpoint is held
     once. Only endpoints placed on the delivering node are returned.
- **Divergences and follow-ups:** this supersedes the plan to hold a component's whole subtree
  and to model a feed as a `power_source_id` column on the component: a subtree hold took
  containment for dependency, and a single column could not name an outlet or a second feed.
  Following `path` edges is #1652; the outlet command type is #1653; a dual-fed device is #1654; holding endpoints placed on another node is #1655; reading an
  outlet's observed off state rather than ending its hold at the `hold_seconds` backstop is #1656.
  The console surface for edges is not built; the API, CLI and MCP carry them.
- **Rejected:** walking `parent_id` (containment is not dependency, above); a column per kind on
  `component` (one upstream per kind, no outlet, no second feed); one generic edge with no kind (a
  power edge and a host edge propagate different things, and the resolver would have to guess);
  holding a host dependent's out-of-band endpoints on a reboot (they answer through it, as the
  target's do); holding the PDU's own endpoints on a power command (the PDU stays up while its
  outlet is off).
