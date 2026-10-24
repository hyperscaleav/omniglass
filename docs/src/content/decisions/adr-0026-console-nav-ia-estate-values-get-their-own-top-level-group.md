---
title: "Console nav IA: estate values get their own top-level group; the Settings group becomes Admin"
date: 2026-07-13
status: "Accepted"
summary: "Console nav IA: Variables, Secrets, and Config get their own top-level Values group; Inventory holds the estate entities including Nodes; Interfaces and Tasks become facet panels; the Settings group is renamed Admin"
---

- **Date:** 2026-07-13 | **Status:** Accepted | **Pages:** [ui](/architecture/ui/)
- **Decision:** The operator console left nav is reorganized around five genera: Catalog (the reusable,
  estate-agnostic model), Inventory (the estate instances: locations, systems, components, and nodes), Values
  (the operator-set values resolved down the scope cascade: variables, secrets, config), the observed surfaces
  (Explore, Alarms, Dashboards, Learn), and platform Admin. Secrets, Variables, and Config are values operators
  set on estate entities, so they move from the Settings menu into a **Values** group of their own, standing
  beside Inventory rather than nested inside it as a band. Config's meaning is fixed as the **CI store**:
  operator-set desired component and system configuration, optionally observed back from the device to detect
  drift and reconcile, distinct from platform Settings and from Variables. Inventory gains **Nodes** (the
  collection daemons, a monitored, scope-controlled entity, ungated "soon" until `node:read` lands) alongside
  Locations, Systems, and Components; Interfaces and Tasks are dropped from the nav entirely, since an interface
  is a facet of a component and a task a facet of a node, not a directory of their own. The Settings group is
  renamed **Admin** (Users, Roles, Groups, Audit) and gains an ungated "soon" Settings leaf that reserves the
  platform-settings-table page.
- **Context:** Settings had become a junk drawer mixing platform governance, platform config, and estate-attached
  values. Those three values attach to a single estate entity on the scope cascade (the same genus as a tag
  assignment) but are not estate entities themselves, so they earned a home of their own, not Settings, not
  Catalog, and not a band folded inside Inventory. This **supersedes** the "into Catalog" line of ADR-0025 above:
  the earlier same-day plan named Catalog, and the decision is a dedicated Values group. Interfaces and Nodes were
  first sketched as Inventory children alongside the estate entities; Nodes stayed (a node is monitored and
  scope-controlled exactly like a location, system, or component, so it belongs with them, not under Admin), but
  Interfaces and the Tasks a node runs were cut from the nav once it was clear each is a facet of one owning
  entity's detail page (a component's device endpoints, a node's collection assignments), not a set an operator
  browses on its own. The relaxed whole-group-drop (an ungated Settings "soon" stub keeps the Admin group visible
  to a viewer, showing only that greyed placeholder while every data-bearing child stays admin-gated and hidden)
  is deliberate until the platform-settings backend ships and the leaf is gated on `setting:read:admin`. Design:
  `docs/superpowers/specs/2026-07-13-operator-console-nav-ia-design.md`.
- **Closes:** issue [#222](https://github.com/hyperscaleav/omniglass/issues/222).
- **Update (2026-07-14):** **Files** joins the **Values** group. The files slice ([ADR-0029](#adr-0029-files-slice-1-a-content-addressed-blob-store-and-a-tenant-wide-file-handle)) first shipped the Files directory under Inventory, but a file is not part of the monitored estate (no health, not polled): it is operator-uploaded **content**. So the Values group broadens from "operator-set values resolved down the cascade" to **operator-set values and content**, with the (deliberately non-cascading, flat) file as its content member alongside the cascaded variables, secrets, and config ([#249](https://github.com/hyperscaleav/omniglass/issues/249)).
