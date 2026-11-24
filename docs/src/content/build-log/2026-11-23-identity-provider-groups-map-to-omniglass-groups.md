---
title: "Identity provider groups map to Omniglass groups (#2363)"
date: 2026-11-23
refs: [2363, 2361, 527]
---

The second single sign-on slice of ADR-2361. An admin maps a value of the provider's groups claim,
keyed by issuer, to a user group (`/user-groups/{group}/idp-mappings`, the generated `omniglass
user-group` mapping subcommands, and an **Identity provider groups** editor on the group blade). A new
migration adds `user_group_idp_mapping` and gives `user_group_member` a `source` (`local` or `idp`)
and, on an `idp` row, its issuer.

Each SSO sign-in sets the person's `idp` memberships from that issuer to exactly the mapped set inside
the sign-in transaction, each add and remove audited; a token with no groups claim is the empty set.
The re-check sweep now verifies the id_token a refresh returns and, when it is the link's own subject
and carries the claim, runs the same sync in the transaction that accepts the refresh, so a group
removed at the provider lands at the next re-check that reflects it. Local rows are never touched and
stand in for an `idp` one. The owner guard runs both ways: no mapping to a group with an owner grant
at any scope, no owner grant on a mapped group.

The roster shows each member's source (`from SSO` with the provider's host). An admin's add makes an
`idp` row local, and removing a group's last mapping from an issuer removes that issuer's `idp`
members at once (ADR-2363). Proven end to end against the fake provider: a mapped claim gives a
location-scoped read in scope and none outside, a sign-in without the claim and a refresh whose token
drops the group both take it away, and local memberships stand.
