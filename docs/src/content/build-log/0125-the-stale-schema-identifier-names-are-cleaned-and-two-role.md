---
title: "The stale schema identifier names are cleaned, and two role tables disambiguated"
date: 2026-07-23
refs: []
form: bullet
---

- **The stale schema identifier names are cleaned, and two role tables disambiguated.** The collapse
  preserved every dependent object name verbatim, so the single init carried constraints, indexes, and
  sequences whose prefixes no longer matched their tables: `metric_datapoint_*` and `state_datapoint_*` on
  the renamed `metric` / `state`, `component_make_*` on `vendor`, `system_type_*` on `standard`,
  `datapoint_type_*` on `property`. Each is brought back in line with its table, and the `#262` artifact is
  fixed too: every registry had a `*_uuid_id_not_null` on its uuid `id` column while the text handle kept
  the old `*_id_not_null`, now `*_id_not_null` on `id` and `*_name_not_null` on `name`. Separately,
  `role_capability` and `role_assignment` become `system_role_capability` and `system_role_assignment`:
  both belong to the AV [system role](/architecture/core-entities/) (a slot a component fills in a system),
  not the IAM `role` table, and the bare `role_` prefix invited exactly that confusion. Identifiers only,
  no behaviour change: the init applies clean and the full suite is green.
