---
title: "A refusal on a readable target tells the truth"
date: 2026-08-14
refs: []
form: bullet
---

- **A refusal on a readable target tells the truth**
  ([#736](https://github.com/hyperscaleav/omniglass/issues/736), ADR-0116). Seventeen routes that
  hang off a scoped tree entity resolved their target with the action's own scope alone, so a
  component, system, or location the caller could **read** but not act on came back as "not found".
  They now make the read-then-action split the tree entities' own CRUD has always made: outside the
  read scope is still the non-disclosing 404, readable but outside the action scope is a 403.
  Property set and clear on all three tiers, system membership add/remove/setPrimary, system role
  declare/withdraw/assign/unassign/swapPositions, and component alarm raise/clear/acknowledge, the
  last being the exact route [#728](https://github.com/hyperscaleav/omniglass/issues/728) hit and
  deliberately left alone rather than ship one route out of step with its siblings.

  **The primitive already existed**, which is what made this an adoption rather than a design:
  `resolveScoped` takes both sets and has since it was written. The audit that preceded the work is
  the reason the slice is this shape. It found three things the issue was wrong about. The matrix
  already asserted the three-way split and already had the read-but-not-act principal, so the work
  was route COVERAGE, not a new principal. The page's own explanation of the gap ("every scoped route
  resolves its target with the action's own scope") was already false, since the tree CRUD split.
  And the real gap was narrower than "every scoped route": it was the routes that hang OFF a tree
  entity, which is why the fix is seventeen call sites and not sixty-eight.

  The **read set is the caller's own `<resource>:read` at every one of them**, checked route by
  route rather than assumed. That is the condition that keeps the 403 honest: it names a row, so it
  may only be reached by a caller who could have read that row anyway. A wider set there would turn
  the truthful refusal into the disclosure the 404 exists to prevent.

  The conformance matrix's registry now carries, per entity, the routes that hang off it, and drives
  all three branches through each. Per entity would have proved five of seventeen: the routes are
  not uniform across the tiers. Two expectations moved and neither was deleted: the acknowledgement
  refusal in `alarm_ack_e2e_test.go` (404 to 403, on a test that already proved the caller could read
  the far alarm) and the membership refusal in `location_grant_reach_e2e_test.go` (404 to 403, the
  one route on that list that disagreed with the three above it). The property and role e2e 404s
  flagged during the audit turned out to be owner-driven misses on rows that do not exist, so they
  stayed.

  The property writes also stopped resolving their owner twice: the split returns the id the
  property arc stores, so `guardOwnerScope` plus `ownerArcValueInScope` collapsed into one resolve.

  Left alone on purpose, each with its own issue: the command-issue route fences its write with
  `component:read` ([#749](https://github.com/hyperscaleav/omniglass/issues/749)), and the node
  placement bind's coverage gap ([#750](https://github.com/hyperscaleav/omniglass/issues/750)) sits
  behind `resolvePlacementRef`, whose cross-tier non-disclosure is a different contract and not this
  one.
