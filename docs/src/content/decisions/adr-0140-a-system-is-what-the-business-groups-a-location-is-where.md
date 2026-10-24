---
title: "A system is what the business groups, a location is where things sit, and a room is one thing"
date: 2026-10-11
status: "Accepted"
summary: "A system is the logical grouping the business cares about and support troubleshoots through; a location is the physical grouping that finds the box on site; operators add components to a system. Every system has a place, which bounds its components (checked against the primary system only) and its subsystems; a component's place is inherited from its primary system unless set, derived at read time; systems group into systems (a divisible hall and its rooms); a place holding one system is one thing with it. The cross-tier scope expansion is decided separately (#10)"
---

- **Date:** 2026-10-11 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/), [health](/architecture/health/), [identity and access](/architecture/identity-access/)
- **Decision:** Three entities, two groupings, both in daily use. A **system** is the logical
  grouping the business cares about: the components that make a thing work, the unit Omniglass
  monitors, and what support troubleshoots through (its health, usually remotely). A **location** is
  the physical grouping: where things sit, and how support and inventory go and find a box on site.
  Operators add components **to a system**; membership is how a component belongs. Then:
  1. **Every system has a place**, the deepest place that spans everything it needs; existing
     placeless systems backfill to the deepest place containing their members, else the root (#876).
  2. **A system's place bounds its components and subsystems**: a component's own place must be at
     or below its **primary** system's place, secondary memberships not constraining it (a shared
     head-end sits beside the rooms it serves); a subsystem's place must be at or below its parent's;
     a system move that would strand an explicitly placed primary member is refused, naming it (#877).
  3. **A component's place is inherited from its primary system unless set**: its effective place is
     derived at read time, never copied into the column, and every reader that means "where is this
     component" reads it (the label, the tag and variable cascade, the fleet view, the console) (#878).
  4. **Systems group into systems**: a divisible hall is one system holding the shared head-end, with
     a subsystem per room it splits into (#879).
  5. **Where a place and its system coincide, they are one thing** (ADR-0139): a place holding exactly
     one system is shown, addressed and configured as that system, its components a drill-down inside
     it; for a divisible hall that holds at both levels.
- **Context:** Operators think in two hierarchies at once, and #872 made the console honour both.
  The model underneath contradicted it in three places: a component's location was only what was
  explicitly set (so a component added to a room's system had no place, and moving the system left
  it behind; the old console copy "location follows the primary system" claimed an inheritance the
  schema never had), a system's location was optional, and system nesting existed in storage with
  no seeded data, no console surface and no health rollup, so the divisible hall it is for could not
  be expressed. Considered and rejected: retiring system nesting (the divisible hall needs it);
  making a system a facet of its location (a place can hold several systems, and a system's
  components span places); checking containment against every system a component belongs to (it
  would forbid the divisible hall's own shared head-end); copying the system's place into the
  component on join (stale on every system move). The scope consequence (a location grant reaching
  the systems and components placed there) is the cross-tier expansion, decided separately (#10).
  The `core-entities` claim that system nesting feeds the health rollup is corrected with this entry;
  the parent-verdict rule is decided with the grouping slice.
