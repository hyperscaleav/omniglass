---
title: "Identity: root-excluded grants and the deploy role"
date: 2026-07-06
refs: []
form: bullet
---

- **Identity: root-excluded grants and the deploy role.** A grant gains an optional `exclude_root` modifier
  that narrows its **modify** actions (update, delete) to the root's descendants: the holder can create under
  and edit within the subtree, but a `PATCH` / `DELETE` on the root itself is a **403** (readable, outside the
  write scope). Read and create-placement keep the root, so a `POST` under the root and a `PATCH` on a child
  still succeed. A new **deploy** official role (create + update on location / system / component, read via the
  viewer floor) is the integrator / field-tech grant: `deploy @ location:room-42 (exclude_root)` populates a
  room without touching its record. The modifier lives on `principal_grant`, resolved in one predicate
  (`inScopeTree`) shared by all three tree entities, not a new scope kind (ADR-0009); an inclusive grant on the
  same root wins. Proven by scope unit tests (per-action classification, inclusive-wins) and a real-binary API
  test (read root 200, create-under-root 201, update child 200, update root 403, delete 403). API and CLI carry
  `exclude_root`; the grant-builder toggle is a fast-follow (#99).
