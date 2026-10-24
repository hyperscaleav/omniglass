---
title: "Identity: principal groups (grant-by-group), backend"
date: 2026-07-08
refs: []
form: bullet
---

- **Identity: principal groups (grant-by-group), backend.** A `principal_group` plus membership, and a grant
  that targets a group (a nullable `group_id` on the one `principal_grant` table, exactly-one-of
  principal/group). A member's effective grants are its direct grants unioned with its groups' grants in the
  grant loader, the single seam both the permission flatten and the gateway scope resolution already read, so an
  inherited grant scopes and flattens exactly like a direct one and no other code changes. Group CRUD, member
  add/remove, and group-grant assignment ship on the Gateway and the API (`/principal-groups`), gated by a new
  `principal_group` capability (admin gets `principal_group:*`); a group grant reuses `principal_grant:create`
  and the same escalation cover-check. Proven against real Postgres: a storage integration test (a viewer @ hq
  granted to a group resolves into a member's read scope and permissions; removal and group-deletion drop it;
  dedup / duplicate-name / idempotent-add hold) and an HTTP end-to-end test (a no-access user gains read once
  added to a group holding viewer @ all and loses it on removal; management is `principal_group`-gated; an admin
  cannot group-grant owner).
