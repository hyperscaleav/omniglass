---
title: "The word for where things sit is `location`"
date: 2026-11-16
status: "Accepted"
summary: "One noun for the physical grouping on every surface: location. Place retires as a noun from the console, the docs and the API's template bodies; placed and placement stay, because they name the relation, not the thing"
refs: [2030, 2028, 2039]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [glossary](/architecture/glossary/),
  [core entities](/architecture/core-entities/)
- **Decision:** (#2030.) The physical grouping is a **location**, classified by a **location
  type**, on every surface: API, schema, permissions, CLI, console and docs. **Place** retires as a
  noun: the console's "Parent place", "Configure place" and "Start places" become location, the
  detail view's place card becomes the location card, the API's `CreatedPlaceBody`,
  `TemplatePlaceBody` and `TemplatePlaceSystemBody` rename to Location, and docs prose follows.
  **Placed** and **placement** stay: they name the relation (a component is placed at a location),
  and no other word does that job. Place as a noun joins the docs denylist. The renames are #2039.
- **Context:** the contract layer already says location everywhere (25 API paths, 24 schemas, the
  `location:*` permissions, the `location` and `location-type` CLI groups, the schema), while
  [ADR-0137](#adr-0137-explore-is-the-fleets-door-and-it-is-an-outline-of-places) and
  [ADR-0139](#adr-0139-a-detail-view-is-one-per-system-its-place-a-card-of-context) brought place
  into the console and docs, so operator-facing text now splits almost evenly (the guides: 220
  location, 173 place) and one console form says both. A reader meeting both has to be told they are
  the same thing, and a word that needs that is the defect the vocabulary epic (#2028) exists to
  end. Location wins because it is already the contract, so only prose and a handful of labels
  move, and because place is ambiguous on live console forms, where it is also a verb (place an
  endpoint on a node) and a number word (decimal places). Considered: **place**, all-in, which reads
  better in a sentence and is shorter, but buys prose with every route, permission, table and CLI
  group, and keeps both collisions; **site**, common in the industry, which names one level of the
  tree, when location types are the operator's own data and no noun may assume a level.
