---
title: "Secret: admin-sensitivity and a scoped, sensitive-off-the-floor directory"
date: 2026-07-13
refs: []
form: bullet
---

- **Secret: admin-sensitivity and a scoped, sensitive-off-the-floor directory.** The second half of the
  visibility rework, for secrets. Two axes now decide who reaches a secret ([#210](https://github.com/hyperscaleav/omniglass/issues/210)):
  **placement scope** (unchanged) gives locality, and a new per-secret **`admin_sensitive`** column flips a
  secret to the **`:admin` tier**, so a platform credential (a Zoom or Microsoft client secret) stays
  admin/owner-only even at the same scope as an operational device secret. The type carries a
  **`default_admin_sensitive`** that seeds the create form (a new `oauth2-client` type defaults sensitive,
  `snmp-community`/`basic-auth` operational). `secret` also joins a **sensitive-resource set** a bare `*`
  does not reach, in both the direct match and the `:read` floor (Go rbac and the client `can()`), so a
  `viewer` (only `*:read`) reads no secrets while `operator`/`deploy` gain a scoped
  `secret:read,reveal,create,update`; `admin`'s `secret:*` becomes `secret:>`. Enforcement is a `canAdmin`
  capability computed at the API and passed to the Storage Gateway: the `/secrets` directory is now
  **scope-filtered** and hides admin-sensitive rows, and reveal/update/delete of a hidden secret is a
  **non-disclosing 404**; creating an admin-sensitive secret needs the admin tier. Proven by a real-binary
  API test (an operator sees and reveals its in-scope device secret but a non-disclosing 404 on the
  admin-sensitive one and an out-of-scope one, and is 403 creating a sensitive one; a same-scope admin and
  the owner see and reveal it), storage integration tests (the scoped list, the seeded type defaults), rbac
  matcher and covers cases, and the nav/`can()` web tests (Secrets hidden from `*:read`, shown to an explicit
  `secret:read`). The move of Secrets, Variables, and Config into Catalog is a separate branch
  ([ADR-0025](/architecture/decisions/#adr-0025-secret-is-a-sensitive-resource-a-per-secret-admin_sensitive-flag-flips-a-secret-to-the-admin-tier)).
