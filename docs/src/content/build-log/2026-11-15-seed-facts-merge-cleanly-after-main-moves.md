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
section the walk does not cover). A merge test in `internal/seed` adds two property types at
different places in one YAML, renders each branch, and requires git's line merge of the two
`seed.json` files to come back clean and equal to a fresh render of the merged YAML. Every other
generator is held deterministic, not merge-friendly: `gen-drift`'s `make gen` must reproduce the
author's committed output byte for byte, and the new `make gen-twice` runs `make gen` twice locally
and fails if the second run moves a byte of the tree. `.gitattributes` marks the generated MCP catalog,
its reference page and the `web/src/lib/*.gen.ts` files, which it had missed, and records that
`plan.go` and `coverage.yaml` are hand-written sources. The recovery after main moves (merge, resolve
the sources, `make gen`, commit) is in [API first](/contributing/api-first/#after-main-moves).
