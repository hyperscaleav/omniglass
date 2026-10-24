---
title: "The telemetry model is typed registries over bare-noun data tables"
date: 2026-07-23
status: "Accepted; superseded in part by [ADR-0079](#adr-0079-five-telemetry-lanes-and-property-stops-being-the-genus); revised in part by [ADR-0141](#adr-0141-a-command-settles-when-its-evidence-arrives-from-its-reply-first-and-observation-second)"
summary: "The telemetry model is typed registries over bare-noun data tables"
---

- **Date:** 2026-07-23 | **Status:** Accepted | **Pages:** [datapoints](/architecture/properties/), [events](/architecture/events/), [variables](/architecture/variables/), [storage](/architecture/storage/), [glossary](/architecture/glossary/)
- **Decision:** every component interaction normalizes to one of three **registries**, each suffixed
  `_type` (`property_type`, `event_type`, `command_type`), over bare-noun **data tables**
  (`metric`, `state`, `property`, `event`, `command`). A registry is a classification, so it takes the
  `_type` suffix; the bare noun holds the instances. This retires the last confusion left by the
  [datapoint_type to property rename](#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle):
  today's `property` registry becomes `property_type` (`kind` in `{metric, state}`), and today's
  `property_value` becomes `property`, the latest-value cache. `event` and `command` gain their own
  registries and are no longer modeled as a `property` kind.
- **Context:** using bare nouns for registries was the root inconsistency. `property` named the
  registry while `property_value` named the data; `event` had been folded into the property registry as
  `kind=log`, the "false unification" [datapoints](/architecture/properties/) and
  [events](/architecture/events/) explicitly warn against; and the code said `property` while the pages
  still said `datapoint_type`. Suffixing the registry `_type` and freeing the bare noun for the data
  fixes all three at once, and it **vindicates the two-registry separation the pages always wanted**:
  `property_type` and `event_type` stay distinct catalogs, they were never one universal registry.
- **The reusable pattern.** A **registry** (`<noun>_type`) defines canonical entries. A **realization**
  (the bare `<noun>`) records data referencing one registry entry by FK, over the same exclusive **owner
  arc** (component / system / location / node) the estate already uses, tagged with a **provenance** or
  **origin**. `metric`/`state`/`property` reference `property_type`; `event` references `event_type`;
  `command` references `command_type`. One rule, four tables, no bare-noun registries.
- **A log is a collection of events, so there is no `log` table.** The row is an `event` (one
  occurrence); a log is the *stream* of them, the way a registry is a collection of keys. So the earlier
  plan to rename the occurrence table `event` to `log` is **reversed**: the table stays `event`, and "a
  component's log" is a **query** over its events (observed origin). Component-observed versus
  platform-derived is an `origin` on the row, not a separate table; promoting a raw line into a typed
  event is enrichment of the row, not a move between tables.
- **The owner arc stays; owner-prefixed tables (`component_metric`, `system_metric`, ...) are
  rejected.** The exclusive arc already carries a component's and a system's metrics in one table, and it
  is the estate's established primitive (`property`, `tag_binding`, `secret`, `variable` all use it).
  Splitting by owner would multiply the firehose tables fourfold, fragment the hot path, and force a
  UNION for the query that matters most: a system's health rolling up its components' metrics wants one
  table. The only gain is a non-null single FK, which the arc's check already enforces logically.
- **`property` is a latest-value cache; the firehose stays `metric`/`state`.** `property` holds the
  newest value per **series**, `(owner, property_type, instance, provenance)`, the same series identity
  the firehose uses, **upserted on intake**. `metric` and `state` remain the append-only samples. The
  cache exists to answer "what is it now" and "what did we last tell it" without scanning the firehose.
- **Provenance in the cache is rows, not columns.** Each provenance is its own series row (`observed`,
  `calculated`, `intended`), so `observed=45` and `intended=50` are two rows, not two columns. Columns
  would put device-intake, command, and config all updating the same row (lock contention, lost updates
  on the hot path) and bake the provenance set into the schema. The "want / told / is" one-liner is the
  right **read** shape, delivered as a **pivot view** over the rows: read-shape is not write-shape.
- **`declared` resolves on demand; `intended` is stored.** The config setpoint (`declared`) is resolved
  live from the cascade and never rows into the cache, because it is always current and needs no history.
  The last commanded value (`intended`) **is** stored, because settlement needs the fact plus its `ts`.
  So the cache's provenance set is `{observed, calculated, intended}`.
- **Two different drifts fall out of that split.** **Command settlement** compares `observed` to
  `intended`, is **windowed**, and is short-lived ("did my last command take?"). **Config drift**
  compares `observed` to `declared` (resolved live), is **ongoing** ("is it where config wants it?").
  This is exactly why `intended` is stored and `declared` is not.
- **The settle window is a driver fact, carried on `command_type`.** How long a command takes to
  actuate (an input switch is near-instant, a lamp warmup is tens of seconds) is device-physical, so it
  lives on the **driver**, on the `command_type` the driver populates (the driver as a declarative menu
  of canonical properties, events, and commands), not on the abstract `property_type`. Settlement is
  **computed**, never a stored flag: within `now - intended.ts < settle_window` the value is pending and
  drift is suppressed; past the window, `observed` matching `intended` is settled and a mismatch is a
  failed command.
- **Staging.** The **name foundation** is cheap and lands first: `property` to `property_type`,
  `property_value` to `property`, and the `metric`/`state` FK repoint, a wide but mechanical sweep with
  no behavior change. The **event family** (`event_type`, the `origin` and causation columns, pulling
  `kind=log` out of `property_type`) rides the calculation and promotion layer, still `Design`. The
  **command pillar** (`command_type`, `command`, the settle window, command settlement) is greenfield.
  Each architecture page is rewritten to this model in the slice that builds its part, per
  [docs with everything](/contributing/docs-with-everything/); until then the pages carry an inline
  note pointing here.
