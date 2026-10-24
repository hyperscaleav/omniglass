---
title: "The catalog finds its sections"
date: 2026-08-07
refs: []
form: bullet
---

- **The catalog finds its sections**
  ([#607](https://github.com/hyperscaleav/omniglass/issues/607)). Fourteen flat Catalog entries
  hid five real clusters; now the rail reads as a taxonomy under one naming rule (ADR-0083): a
  section is named for the estate noun it serves, an entry keeps the registry's own word, and
  where the registry's only word is "type" the entry is Types with the section completing the
  sentence. **The sectioned rail** (#608) renders non-folding headers from the permission-filtered
  entry list (a fully gated section disappears with its entries), tags sectioned palette entries
  `Catalog · <section>`, spells the full address in the top bar, adds the Logs and Notifications
  soon stubs, and walks Templates off the rail while a nav-path binding test pins that every
  unlive entry resolves to a registered stub. **The overview hub** (#609) opens `/catalog` from a
  visible entry: one card per section derived from the same navItems the sidebar renders and
  filtered through the same permission logic, live registry counts through the list pages' shared
  query keys, each registry its own query so one failure marks its own row. Its review retired a
  vacuous leak-pin (a synchronous not-called fetch spy that the client's async middleware made
  unfailable) for a query-cache assertion, and the vacuous-async-spy class joined the
  anti-pattern catalog. **The map** (#610) recorded ADR-0083 with the rejected shapes (flat rail,
  hover flyouts, uniform Types suffix) and the #606 fence around the rules/alarms vocabulary.
  Organizing line, taught on the UI page: Telemetry is what gets recorded, Action is what the
  platform does.
