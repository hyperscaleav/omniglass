---
title: "Estate: technical-name rename with an inline name check"
date: 2026-07-14
refs: []
form: bullet
---

- **Estate: technical-name rename with an inline name check.** A component, system, or location's
  technical name (its `name`, the URL address) is now editable from the detail accordion in edit
  mode rather than fixed at creation. Because `id` (uuidv7) is the identity and every foreign key,
  tag/variable/secret binding, and placement references that UUID, a rename is a one-column update
  with no cascade. A shared `ValidateEntityName` slug rule (`^[a-z0-9][a-z0-9-]*$`) is the server
  source of truth, mirrored client-side, and now gates create as well as rename. An inline **Check**
  button calls a collection-level `POST /<entity>:checkName` that reports `{valid, available,
  reason}`; availability is deliberately **scope-blind** to match the global unique constraint (a
  scope-aware check would false-positive on a name held outside the caller's scope) and is gated by
  `<entity>:update`. The check is advisory: Save stays enabled and the unique constraint (409) is the
  real gate. `make gen` carries the new field and method through to the typed client and CLI. Proven
  by the rename round-trip per entity (a child or binding's UUID foreign key still resolves after the
  parent renames, and the old name frees for reuse), dup-name 409, bad-slug 422 at both create and
  rename, the three checkName states, and a scope-blind test (a `deploy` principal sees an
  out-of-scope name as taken, not available), plus per-page web tests for the editable field and the
  read-only view ([#245](https://github.com/hyperscaleav/omniglass/issues/245)). Deferred: an
  old-name to id redirect for bookmarked links (a soft 404 today), and wiring the check into the
  create draft's live validation.
