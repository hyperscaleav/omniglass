---
title: "The type-registry references carry both forms"
date: 2026-07-23
refs: []
form: bullet
---

- **The type-registry references carry both forms**: the `interface`, `location`, and `secret` bodies
  gained `type_id` / `location_type_id` / `secret_type_id` beside their name (`type` / `location_type` /
  `secret_type`), which were previously name-only, an inconsistency with the both-forms rule the epic
  enforces everywhere else. `TestReferencesCarryBothForms` now guards **both directions**: the forward
  scan already caught a `*_id` with no name; a reverse pass over a curated set of unambiguous registry
  name-fields (`location_type`, `secret_type`) now catches a registry handle with no id, closing the gap
  that let those name-only references slip past (the forward scan has no `*_id` field to trip on). `type`
  stays forward-covered on the primary interface body alone, since it also names an RFC-9457 error type
  and a secret field's data type. The `property_name` contract and value bodies are a follow-up.
