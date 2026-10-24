## hand-written-fact (live)

- **Cue:** the diff adds a hand-written artifact restating a fact the code already knows:
  a route/column/env-var/seeded-row table, a copied schema, an asset a generator could
  emit.
- **Failure:** drift waiting to happen; the audit found the generated artifacts were the
  only ones that had never drifted.
- **Fix:** the generate-first rule (CLAUDE.md): a generated render in the same PR, or a
  filed issue named in the ship-review.
