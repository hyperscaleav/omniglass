---
title: "Identity: the Roles view and role metadata"
date: 2026-07-07
refs: []
form: bullet
---

- **Identity: the Roles view and role metadata.** Roles gain a `display_name` and `description` (additive
  migration, seeded for the five official roles), and `GET /roles` now returns them plus each role's
  **effective permissions** (flattened through the role index: inheritance, wildcard, and the `:read` floor
  resolved). The console gains a read-only **Roles** page (`Settings > Roles`): a self-teaching catalog of each
  role's display name, description, inheritance, and effective permissions, ordered by tier, rendered from the
  real seeded roles. The **grant builder** role picker gains a hover tooltip showing a role's description and
  permissions, so an admin sees what a role grants while assigning it. No custom-role CRUD (a later slice; the
  ADR-0013 cover rule must extend to role editing then). Proven by a real-binary API test (owner effective set
  is `*:*`, admin's is broad but not `*:*`, viewer's includes `*:read`, operator inherits the read floor),
  storage round-trip, and web tests (the Roles page renders and tier-orders the seeded roles).
