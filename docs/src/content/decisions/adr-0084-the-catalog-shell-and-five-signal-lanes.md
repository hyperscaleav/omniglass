---
title: "The catalog shell, and five signal lanes"
date: 2026-08-07
status: "Accepted"
summary: "Catalog is one rail entry opening a shell: a grouped subrail (Telemetry, Actions, Components, Systems, Locations, Metadata) navigating to the per-registry pages at canonical URLs, with an Overview landing; the organizing axis is direction (Telemetry is what you receive, Actions what you send or run), the lane collective noun becomes five signal lanes, and secret types loses its nav slot"
---

- **Date:** 2026-08-07 | **Status:** Accepted | **Pages:** [UI](/architecture/ui/),
  [glossary](/architecture/glossary/)
- **Decision:** Catalog is a single rail entry opening a **shell**: a grouped subrail whose
  entries navigate to the real per-registry pages, rendered in the pane at their canonical flat
  URLs, with an **Overview** landing of teaching cards; the subrail and the Overview derive from
  one group table, judged through the same permission filter the rail uses. The groups, ordered:
  **Telemetry** (metrics, properties, events), **Actions** (commands, with rules and
  notifications as tracked stubs), **Components** (vendors, products, drivers, capabilities, a
  templates stub), **Systems** (standards, a templates stub), **Locations** (types, a templates
  stub), **Metadata** (tags). The organizing axis is **direction, not genus**: Telemetry is what
  you receive, Actions what you send or run; a command targeting a property or metric is a form
  dependency, not a menu adjacency. Consequences ruled with it: **secret types loses its nav
  slot** (the URL stays reachable and gated; the table's retirement is the schema phase's call);
  **Logs stays out** of Telemetry until a log_type exists; the Systems entry reads **Standards**
  until the system_blueprint rename lands with the schema phase, because splitting operator
  vocabulary across surfaces to ship a label early is drift by construction; and the blade model
  holds on every field (read facts until the pencil, per the epic's approval caveat). The lane
  collective noun becomes **five signal lanes**, four inbound and one outbound: a command is an
  instruction you issue, not a telemetry reading, so "five telemetry lanes" retires as prose
  while ADR-0079's structure (a command is a genuine peer lane: type table, instance table,
  registry) stands unrevised.
- **Context:** the sectioned rail (ADR-0083) shipped its sections as rail geography and a hub
  that restated the rail; four design rounds against the live console replaced it: a
  single-surface browse table (identity-first rows, rejected for parallel-table drift against
  the real pages), a filter subrail (rejected because facets that filter one merged table
  cannot host each page's own search and create flows), and finally the shell, which keeps the
  IA as wayfinding and the pages as the single surfaces. Grouping by subject entity rather than
  artifact class survived every round: an operator arrives knowing the entity, not the taxonomy,
  and a component template and a location template share only a word. ADR-0083 is superseded;
  its estate-noun instinct survives in the group names. The genus-naming discussion that
  produced the direction axis also queued the schema phase (system_blueprint, location contract
  removal, secret_type retirement, the four-class taxonomy), deliberately sequenced ahead of the
  #379 migration collapse and deferred from this decision.
