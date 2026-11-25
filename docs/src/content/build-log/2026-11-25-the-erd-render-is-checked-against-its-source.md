---
title: "The ERD's committed render is checked against its source (#2625)"
date: 2026-11-25
refs: [2625, 2075, 2363]
---

The data-model page inlines a pre-rendered SVG of the ERD, and `TestERDMatchesSchema` only checked the
D2 source, so a schema change left the picture stale: #2075 left `node` and `node_log` drawn after
the rename, and `user_group_idp_mapping` (#2363) was never drawn. `pnpm diagrams` now ends each SVG with a
comment holding the SHA-256 of the D2 block it rendered, and `erd.RenderStale` compares that hash with the
page's source in `make test`, with no Postgres and no `d2` binary. A first version compared the identifiers
each side names, which cannot see an edit that reuses existing names (a column moved to another table or
retyped); the hash sees every edit, and `erd.RenderDrift` remains to name what differs in the failure
message. The check failed on main for the missing mapping table, whose render is refreshed here, and the
page and the `docs-diagram` skill say to re-render after a schema change.
