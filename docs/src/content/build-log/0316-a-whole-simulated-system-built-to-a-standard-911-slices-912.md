---
title: "A whole simulated system, built to a standard (#911, slices #912 and #913)"
date: 2026-10-18
refs: [911, 912, 913]
---

An operator adds a simulated system in one step: `POST /nodes/{name}/simulation/systems` (`omniglass
simulation add-system`) builds a system to a standard, staffing each of the standard's roles with a
simulated product that fits its accepted types and pins (`internal/simfleet.PlanSystem`, pure), and
creates the system, the components, their endpoints and the role assignments in one transaction. The
system, component, role and endpoint creates were split into a public begin-and-commit wrapper over a
`...Tx` body, so the composite is the ordinary creates with the caller's own scopes and audit rows,
committed once. `GET /simulation/standards/{standard}:plan` previews it, roles nothing simulated fits
listed short with the reason. The architect ruled the platform's standard is the archetype and names are
platform-minted; the lab's room archetypes stay only as the fleet compiler's input (#914). The
real-simulator e2e adds a meeting room through the CLI and waits for the simulator to serve every
component. The console's **Add simulated system** (#913) previews the plan as the standard is chosen
and adds the system from the Simulation page.
