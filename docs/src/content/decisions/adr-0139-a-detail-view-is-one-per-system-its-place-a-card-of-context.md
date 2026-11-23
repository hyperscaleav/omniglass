---
title: "A detail view is one per system, its place a card of context"
date: 2026-10-05
status: "Accepted"
summary: "Systems are the unit Omniglass monitors; places are folders and metadata; components are pieces of systems. A detail view is one per system, its place a card of context above the system card and its tabs; a place holding exactly one system lands on it, and a place holding none or several keeps its own view (a brief card per system, then the outline rooted there). The side panel is the glance (verdict, why, context, identity and tags), configuration staying on Configure, revising #826's one-form panel. The shipped system label rule leads with the place, and healthy is grey on every health pill (#872)"
---

- **Date:** 2026-10-05 | **Status:** Accepted | **Pages:** [UI](/architecture/ui/), [design system](/contributing/design-system/), [core entities](/architecture/core-entities/), [glossary](/architecture/glossary/), [health](/architecture/health/)
- **Decision:** Systems are the unit Omniglass monitors; places are folders and metadata about
  where; components are the pieces of systems. So a detail view is one per system: the place as
  a card of context (its type, tags and its own panel), then the system card (verdict, since,
  standard, size, slot arithmetic only while short) with its Overview, Activity and Configure.
  A place holding exactly one system has no view of its own: its address lands on the system,
  query kept, and its place is configured there, in a Place section of the system's Configure
  tab with its own Edit (`?edit=1` on the place becomes `edit=place`). A place
  holding no system, or several, keeps its own subject card with a brief card per system and
  the outline rooted there. A component's view carries its place card, marked as its own or as
  its system's standing in. The side panel is the glance an operator lands on from a row:
  verdict, since, why with its severity, the context they came for, then the form's identity,
  classification, a location's parent and tags; the configuration panels (roles, properties and
  their cascade, reconciliation, interfaces) stay on Configure, which revises ADR-0137's "the
  blade is the one form". The shipped system label rule leads with the place, so a room and its
  system read as one name; a place holding several systems names each one's kind after the place
  (a new `SharesLocation` fact, its siblings restamped by the create, move or delete that changes
  the count). Healthy is grey on every health pill, strip and marker, the
  outline's rule carried to its edge. There is no counts line above a detail view.
- **Context:** The audit for #872 found the workspaces still speaking the idioms the outline
  retired: a location page that was a band canvas, a room and its system as two views under two
  names ("Meeting Room" titled under a "Briefing Room" crumb), three frames for one idea, facts
  said twice (the slots in the counts line and the header, the metrics as tiles and as a table,
  the history on two tabs), and a panel that was a configuration form. The architect's ruling
  on the model was that it is right (locations, systems, components; systems a logical group
  usually bound to one place) and the orientation was wrong: people care about their systems.
  This changes presentation and one shipped label default only; whether a system's place
  should be stored or derived, and the inheritance of a component's place from its system
  (which the UI's old "location follows the primary system" claimed and the schema does not
  carry), stay with #862.
