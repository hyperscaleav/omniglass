---
title: "An interface is a device API; the interface type is its transport, not its driver"
date: 2026-07-08
status: "Accepted"
summary: "An interface is a device **API** named by its protocol (not a NIC); `interface_type` = its **transport** (the reach gate), a **driver** = the collect layer (protocol handler + transports + normalized menu, what a device CAN do), a template **curates** (SHOULD), the instance holds what **IS** there; OIDs/commands live in the driver, not the template"
---

- **Date:** 2026-07-08 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [nodes](/architecture/nodes/)
- **Decision:** An `interface` is an **API endpoint we intend to call** on a component, identified by the
  **protocol it speaks** (`web`, `qrc`, `ttp`, `snmp`), not a network interface; a host or IP is a variable it
  consumes, not its identity. It is named by that protocol and is unique within its component
  (`unique(component, name)`), never a hand-typed label. Two axes are **decoupled**: the **transport** (how bytes
  move) and the **driver** (the protocol handler that produces the normalized functions and datapoints).
  `interface_type` is the **transport** (`ssh`, `tcp`, `http`, `snmp`, `udp`, `telnet`, `icmp`): a node-side wire
  capability that also carries the default **reachability** probe (tcp/ssh/http open the port, icmp pings).
  Reachability is the **first gate of a ladder** (reach to auth to responds to collecting) and needs only the
  transport. A **driver** is the **collect** layer: a protocol handler plus the transport(s) it can run over plus
  the normalized catalog (functions and datapoints, how to fetch them as commands/OIDs/paths, parse, a version).
  The same handler can run over several transports (a CLI over `ssh` or `telnet`), so the driver declares its
  transports and the instance picks one; a genuinely different grammar over a different transport (an ssh CLI vs a
  tcp JSON-RPC) is a **different driver** producing the same catalog. Device-specific fetch detail lives in the
  driver, never the template: `snmp` is the transport, a `biamp-snmp` (or `generic-snmp`) **driver** holds the OID
  map. The entities then split on **CAN / SHOULD / IS**: the **driver** owns what a device family CAN do and how
  (transports, catalog, normalization, discovery rules, version); a **template** (per model) owns what an operator
  SHOULD watch and how it looks (curate the driver's menu to a default subset, thresholds and event rules, an
  icon); the `interface` instance owns what IS actually there (transport, host, credentials, a driver when it
  collects, the discovered subset, per-device overrides). Discovery is a driver rule whose **result lands on the
  instance**; filtering-for-choice is a template default plus an instance override; capability is the driver. The
  reusable driver is **data on one generic engine** (a declarative `canonical datapoint <- fetch <- parse`),
  official or org-custom via the `(namespace, id)` shadow registry, with a pluggable-Go escape hatch only for a
  wire the engine cannot express; a "device pack" bundles a driver plus a template, and a template **declares its
  driver deps** (version-pinned) so a missing or shadowed driver surfaces, never silently misbinds. The house
  `<entity>` / `<entity>_type` pattern holds: `interface_type` is the transport (a reachability interface's type
  genuinely is its transport), and `driver` earns its own registry (SNMP and multi-transport protocols prove it
  folds into neither transport nor template).
- **Context:** The 5a build named interfaces by a hand-typed string (`boardroom-tcp`) with `type` = the probe
  (tcp/icmp), which conflated identity with transport and implied operators name and wire-configure devices by
  hand. The reframe: operators are not programmers, so the value is a **driver that normalizes a device family
  into a pick-from menu**, which makes the template a light curation, policy, and presentation layer and means the
  operator never authors a protocol. You cannot cleanly split "how you talk" from "what you say" (the command is
  both), so the seam is elsewhere: the **transport** is the reusable connection, the **driver** is the reusable
  normalized menu over it, and the **template** is a selection plus policy. Keeping the driver as data (not Go per
  family) is what makes it community-shippable;
  growing the canonical menu device-by-device (not a universal ontology up front) is what keeps it honest;
  separating menu-of-types from discovered-instances is what fits programmable devices (a DSP's blocks are
  per-install); versioning the driver is what lets a template's picks resolve as the menu matures.
- **Scope now (tier-0):** this slice (#114) ships only the first gate. `interface_type` is the transport
  primitive (`icmp`, `tcp`, `ssh`, `http` seeded `built`), each carrying a tcp-connect or ping reachability probe;
  an `interface` is named by its protocol and typed by its transport; the dev seed models a lab **polaris DSP**
  with a `web` (http) and a `qrc` (tcp) interface, the "two APIs on one device" story. The driver catalog,
  normalization, discovery, templates, versioning, and the shadow-resolved device pack are later slices of the
  [collection epic](https://github.com/hyperscaleav/omniglass/issues/113) (slices 2 to 4 realize this model).
- **Refines:** [ADR-0038](#adr-0038-the-reachability-verdict-is-a-built-in-state) (the reachability verdict is the
  first rung of the gate ladder this ADR names).
- **Status note (2026-07-08):** the `interface = API` / `interface_type = transport` half is **built and stable**
  (this slice). The **driver / collect layer** (the separate `driver` entity, the normalized menu, and the
  driver-centric split itself) is **under active design**: it departs from the original template-centric
  architecture (where protocol handling lived in the template), which is a serious enough change to redesign
  deliberately rather than on momentum. Recorded here as the current-best direction, **not a locked gate**;
  driver-centric vs template-centric is re-examined, and this ADR revised or superseded, in a later ADR before
  the collect layer is built.
