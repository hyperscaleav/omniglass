---
title: "Derived seed facts merge cleanly when main moves (#1492)"
date: 2026-11-15
refs: [1492, 1095]
---

Two PRs that each added a seed row from the same base could force a hand merge of the generated
`seed.json`, or merge clean and fail `gen-drift`. The emitter already wrote one entry per line with
map keys sorted, but nothing held it there. `cmd/seedgen` now has tests that render several times
and require identical bytes, require the canonical one-entry-per-line layout, and walk the template
type beside the output to prove every map's keys are sorted (and fail if an object appears in a
section the walk does not cover). Two branches that each add a different property type and
regenerate merge without a conflict in `seed.json`. `.gitattributes` marks the generated MCP catalog,
its reference page and the `web/src/lib/*.gen.ts` files, which it had missed, and records that
`plan.go` and `coverage.yaml` are hand-written sources. The recovery after main moves (merge, resolve
the sources, `make gen`, commit) is in [API first](/contributing/api-first/#after-main-moves).
