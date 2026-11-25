---
title: "The ERD's committed render is checked against its source (#2625)"
date: 2026-11-25
refs: [2625, 2075, 2363]
---

The data-model page inlines a pre-rendered SVG of the ERD, and `TestERDMatchesSchema` only checked the
D2 source, so a schema change left the picture stale: #2075 left `node` and `node_log` drawn after
the rename, and `user_group_idp_mapping` (#2363) was never drawn. `erd.RenderDrift` now compares the
D2's structure with the SVG's (clusters, tables, each column's type and key label, and each
connection, read from the render's text and its base64 group names), both ways, and `TestCommittedERDRenderMatchesItsSource` runs it on
the committed files in `make test` with no Postgres and no `d2` binary. It failed on main for the
missing mapping table, whose render is refreshed here (`pnpm diagrams`). The page and the
`docs-diagram` skill say to re-render after a schema change.
