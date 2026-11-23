---
title: "A fleet blade renders its detail page's body"
date: 2026-11-23
status: Accepted
summary: "A component, system or location opened in a blade renders the same body its detail page renders (`ComponentDetail`, `SystemDetail`, `LocationDetail`, each taking `{ id, host }`), reflowed by container query in a wide (about 720 px) blade. The host holds only the tab (the page's `?tab=`, the blade's own signal), the edit intent and where a Delete leaves to. Reverses ADR-0139's \"the side panel is the glance\""
refs: [2423, 872]
---

- **Date:** 2026-11-23 | **Status:** Accepted | **Pages:** [design system](/contributing/design-system/), [UI](/architecture/ui/), [work with an entity](/operator/entities/)
- **Decision:** A fleet kind has one detail body, mounted by its page (`host="page"`) and by its
  blade (`host="blade"`): every section, tab and action the page shows, the blade shows. The
  page is the address around the body (breadcrumb, name-shaped and sole-system landings, the
  miss faces, its own blade stack); the blade adds its title and Expand. The host decides three
  things only: where the active tab lives (`lib/tabstate`: the page's `?tab=`, a blade's signal,
  so a blade never rewrites the page beneath it, and a rooted outline in a blade filters the
  same way), whether Edit answers the address's `?edit=`, and where a Delete leaves to (a blade
  closes its stack, a page goes to Explore). Editing is the Configure tab's `ConfigureFace` in
  both hosts, with Delete in its footer. Sections lay out by container query, never the viewport,
  and the fleet blade is the wide variant (`BladeDef.width: "wide"`, about 720 px). A body opens
  related entities on the ambient blade stack, so a nested open stacks in either host.
  Tabs stay tabs in a blade, in the same order with the same names, and Expand stays: the page
  still has the breadcrumb and a shareable address. `fleetBlades.guard.test.ts` fails if a kind
  registers a blade body that is not its detail body.
- **Context:** ADR-0139 (#872) made the side panel the glance: health, why, context, identity and
  tags, with configuration and depth one Expand away. In the 2026-11-23 console review the
  architect ruled the reduced panel of little use: "the drawers and the detail pages would be
  identical. We would just push the detail pages into the drawers. The wrapping would certainly
  look different [...] we shouldn't have to go to the detail page to get information about an
  endpoint or a component." The two implementations had also drifted (the page lacked the
  blade's Troubleshoot and by-rule notes, the blade the page's vitals, collection, members,
  history and configuration), and `Files.tsx` already showed one body serving two hosts.
