---
title: "Identity slice 3b: admin update a principal"
date: 2026-07-06
refs: []
form: bullet
---

- **Identity slice 3b: admin update a principal.** `PATCH /api/v1/principals/{id}` edits a human's display
  name, email, and **username**, gated by `principal:update` (all-scope). Renaming is safe by construction:
  nothing keys on the username (credentials and grants reference the `principal.id` uuid), so a rename
  re-homes the login rather than breaking it. A non-human target is 422, a clash 409, an unknown id 404, a
  location-scoped admin 403. The console Users detail panel gains an **Edit** form; the CLI gains
  `principal update`. Proven by a storage round-trip (the rename re-homes authentication), a real-binary
  integration test (allow, scoped-deny 403, 409, 404, 422), and the client unit. Deferred: disable / delete,
  role assignment, and editing a service account's label.
