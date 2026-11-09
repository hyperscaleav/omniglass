---
title: "Platform health is a page behind node:read, not a dashboard"
date: 2026-11-09
status: Accepted
summary: "Accepted: #1644 ships Platform health as a hand-written page over a composed read, GET /views/platform, not a ViewResult dashboard. Its door is node:read, because its rows are the node list, and a scoped node read is a 403, not an empty table. Figures, verdicts and the server's identity need a fleet-wide component read and are omitted short of it. The server's start time is an RFC 3339 string, since there is no timestamp property type, and the view leaves the platform system out rather than writing it null. The Home tile carries the same gate."
refs: [1644, 1643, 523, 1625, 1627, 1646]
---

- **Date:** 2026-11-09 | **Status:** Accepted | **Pages:** [views](/architecture/views/), [ui](/architecture/ui/), [api](/architecture/api/)
- **Decision:**
  1. **A page, not a dashboard.** Platform health is `PlatformHealth.tsx` over `GET /views/platform`,
     a composed read like `GET /views/fleet`, not a `ViewResult` the dashboard engine renders. The
     dashboard engine is #523's; converting this page is a later cut of it, and building a second
     renderer contract here would fork it. The page holds no key list: the view returns the column
     groups (`storage.PlatformViewGroups`, the one place they are written) with each key's catalog
     label, unit and description, so a key added there is a column on the page with no web change.
  2. **The door is `node:read`.** The rows are `ListNodes`, which reaches a node only through a
     fleet-wide read, so the route and the nav entry are gated `node:read`, and a location-scoped
     `node:read` is a 403, as `/nodes` answers it, not an empty table that reads as "no nodes".
     The console never retries a 4xx on this read: the page and the Home tile name the fleet-wide
     node read the caller lacks at once, instead of a loading line that outlasts three retries.
  3. **Each tier is scoped on its own.** `component:read` is the figures, the node verdicts and the
     server's identity; `system:read` is the platform verdict. The platform components belong to no
     place, so anything short of a fleet-wide read reaches none of them, and the view says so with
     `figures_read: false` and leaves the figures out instead of zeroing them. A 0 would claim the node
     measured nothing wrong.
  4. **`server-started-at` is a string.** The ticket specified a timestamp property type; the catalog
     has none (a property's `data_type` is string, int, float, bool or json), and adding one is a
     property-lane change of its own. The start time is a string held to RFC 3339 UTC by its validation pattern,
     parsed by the view into `started_at`. A timestamp type, when one lands, replaces the pattern.
  5. **The platform system is absent, not null.** The ticket's body had `system` null without a
     fleet-wide system read. Huma cannot mark a struct reference nullable, so the field is omitted
     instead; the page treats absent and null alike.
  6. **The Home tile has the same door.** The Collectors placeholder became Platform: the platform
     verdict and the count of nodes whose pill is not up, linking to `/platform`, shown only to a
     caller holding `node:read`. Whichever of this and #523's Home v1 lands second carries the tile
     into the other.
  7. **A node opens two ways.** A row opens the node's platform component, the ordinary component
     detail, where each figure has its history. A secondary link opens the node's blade on the Nodes
     page through a new `?n=<name>` deep link, so editing stays where it lives.
- **Context:** #1643 asks the platform to be monitored the way a fleet is. Every figure the page shows
  was already a sample on a platform component (#1625, #1626, #1627); what was missing was one read
  of all of them and a surface an operator opens first. The server panels (#1645), the Loss group
  (#1646) and the simulation section (#1647) add to this page.
