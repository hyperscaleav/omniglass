---
title: "Every nullable column a PATCH writes can now be cleared (#1189)"
date: 2026-11-13
refs: [1189, 522, 260]
---

A standard's parent clears with `parent_standard_id: ""`, and a metric type's `precision`, an event type's `payload_schema`, a property type's `validation` and a command type's `params_schema` clear by naming the field in `update_mask`, the same convention the location type's `name_rule` set under ADR-0106. Omitting any of them still leaves it unchanged. The four catalog routes now take the full AIP-134 mask over every patchable field, and the storage writes use a `CASE` rather than a `coalesce`, which could set a column but never clear it. Each route has a failing-first end-to-end test and each `Update*` has a storage test for the three states.
