---
title: "Identity: address a principal by username or uuid"
date: 2026-07-09
refs: []
form: bullet
---

- **Identity: address a principal by username or uuid.** Every `/principals/{id}` route (read, update,
  grants, the lifecycle verbs, reset, impersonate) now accepts either the principal's uuid or a human's
  current username. A new gateway primitive `ResolvePrincipalRef` resolves it (a uuid passes through
  unchanged, so an unknown uuid keeps its existing not-found handling; otherwise a username lookup, and an
  unknown username is a 404), and each handler resolves at the top, before any self-check (so addressing
  yourself by username is caught for a reset or impersonation). The uuid stays the stable identity; a
  username is a convenience address resolved at call time. Service principals have no username and stay
  uuid-addressed. This makes the CLI usable without a uuid lookup first (`omniglass principal archive
  alice`). Proven by a storage test (username resolves, a uuid passes through, an unknown username is not
  found) and an API test (get by username matches get by uuid, a `:verb` method archives by username, an
  unknown username is a 404).
