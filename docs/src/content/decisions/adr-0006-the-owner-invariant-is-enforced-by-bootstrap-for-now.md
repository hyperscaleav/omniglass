---
title: "The owner invariant is enforced by bootstrap for now"
date: 2026-06-27
status: "Resolved"
summary: "The single-owner invariant is now a DEFERRABLE constraint trigger, landed with grant revocation"
---

- **Date:** 2026-06-27 | **Status:** Resolved (identity slice 3c) | **Pages:** [identity and access](/architecture/identity-access/)
- **Resolved:** The `DEFERRABLE INITIALLY DEFERRED` constraint trigger (`principal_grant_owner_guard`) shipped with grant revocation ([issue #82](https://github.com/hyperscaleav/omniglass/issues/82)): it refuses to leave zero `owner @ all` grants at `COMMIT`, so revoking the last owner is a clean 409 while a swap (grant a new owner + revoke the old in one transaction) still passes. The gateway maps its custom SQLSTATE `OG001` to `ErrLastOwner`.
- **Decision (divergence):** "At least one active `owner@all` grant exists at all times" is upheld today by
  the bootstrap path (it always creates one) and the absence of any grant-revocation surface. The design's
  **deferrable Postgres constraint trigger** that enforces it at `COMMIT` (so the swap-owners-in-one-txn
  pattern works) is **not yet built**.
- **Context:** With no API to revoke a grant or delete a principal, the last-owner removal the trigger
  guards against is not yet reachable, so the trigger is not load-bearing until grant CRUD ships. It is
  required before the admin user-management slice exposes grant revocation
  ([epic #27](https://github.com/hyperscaleav/omniglass/issues/27), slice 3).
- **Closes the gap:** epic [#27](https://github.com/hyperscaleav/omniglass/issues/27).
