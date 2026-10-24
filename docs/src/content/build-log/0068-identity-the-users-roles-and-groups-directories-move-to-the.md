---
title: "Identity: the Users, Roles, and Groups directories move to the admin tier"
date: 2026-07-13
refs: []
form: bullet
---

- **Identity: the Users, Roles, and Groups directories move to the admin tier.** The directory reads of
  `principal` (list, get, and the profile-picture route), `role`, and `principal_group` are promoted from a
  two-token `<resource>:read` to the admin-sensitive **`<resource>:read:admin`**, so the `viewer` read floor
  (`*:read`) no longer reaches the Users, Roles, and Groups pages, on the console or the API. This supersedes
  the earlier `role:read` / `principal_group:read` / `principal:read` gates named in the slices above. `admin`
  carries an explicit `<resource>:read:admin` alongside its wildcards, the same shape as `principal:purge:admin`;
  `owner`'s `>` is unaffected, and create, update, and the lifecycle verbs stay two-token. Proven by a
  real-binary API test (a viewer@all 403s on `/principals`, `/roles`, `/principal-groups`; admin and owner
  200), the nav filter's web test (the three tabs hidden from `*:read`, shown to admin), and rbac matcher
  cases ([ADR-0023](/architecture/decisions/#adr-0023-the-iam-directory-reads-principal-role-principal_group-are-admin-tier)).
  Secrets, which an operator legitimately reads in scope, are handled by a later slice.
