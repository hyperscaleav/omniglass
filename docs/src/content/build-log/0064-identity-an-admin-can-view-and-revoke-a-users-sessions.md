---
title: "Identity: an admin can view and revoke a user's sessions (slice 2 of 2)"
date: 2026-07-13
refs: []
form: bullet
---

- **Identity: an admin can view and revoke a user's sessions (slice 2 of 2).** An administrator can now
  see and end **another** principal's sign-ins and API tokens, so a lost laptop or a leaked token is cut
  off without resetting the account. No new storage: it reuses slice 1's `ListBearerCredentials` (passing a
  nil `currentHash`, so no row is ever flagged `current` when viewing someone else) and the
  principal-scoped `RevokeBearerByID`. A new **`principal:revoke-session`** capability (a normal two-token
  permission, held by `admin` and `owner` through their `principal:*` / `>` wildcards, kept separable for a
  future help-desk role) gates both `GET /principals/{id}/sessions` and
  `POST /principals/{id}/sessions/{sid}:revoke`. The revoke is bounded to the target (a `sid` that is not
  theirs is a non-disclosing 404), sits behind the same **takeover guard** as impersonation and the
  password reset (an owner's sessions cannot be revoked by a lesser admin, nor a principal whose
  capabilities exceed the caller's), and is **audited with the acting admin as the actor**. The console's
  **Users** blade gains **Sessions** and **API tokens** sections (a shared `SessionsList` now backs both
  them and the self-service Profile card), hidden unless the caller holds the capability, with a **Revoke** per row.
  The blade's action-rail kebab also offers **Revoke all sessions** and **Revoke all tokens** (each confirmed),
  bulk-ending one purpose at a time through a new `POST /principals/{id}/sessions:revokeAll` backed by a
  purpose-filtered `RevokeBearersByPurpose` (revoking sessions never touches tokens), returning the count and under
  the same gate, takeover guard, and audit as the single revoke. The generated CLI gains
  `omniglass principal sessions <id>` / `principal revoke-session <id> <sid>` /
  `principal revoke-all-sessions <id>` (the cligen `nameOverride` seam groups them under `principal` so they do
  not collide with the self-service `session` commands, which share the leaf noun). Proven by real-binary API
  tests: an admin lists a target's sessions and revokes one so it stops authenticating, bulk-revokes all sessions
  (both cookies stop authenticating, the tokens survive) then all tokens, an operator is 403, revoking an
  **owner's** session or bulk-revoking its tokens as a lesser admin is 403 (the takeover guard), a `sid` that is
  not the target's (or an unknown principal) is a 404, a bad purpose is a 422, the secret never appears, and the
  revoke is audited with the admin as actor.
