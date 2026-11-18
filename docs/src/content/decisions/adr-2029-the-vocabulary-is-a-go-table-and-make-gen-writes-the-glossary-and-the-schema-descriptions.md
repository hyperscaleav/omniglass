---
title: "The vocabulary is a Go table, and make gen writes the glossary and the schema descriptions"
date: 2026-11-16
status: "Accepted"
summary: "Every product word an operator or an admin reads is one row of internal/vocab/terms.yaml (word, plural, definition, API collection, schema, permission noun, nav entry, verdict, retired words). make gen renders the docs glossary from it, and a post-pass at the end of route registration sets each mapped OpenAPI component schema's description to its term's definition. Retired words move from the docslint hand list into the table, and guards fail an unmapped collection, nav entry or verdict"
refs: [2029, 2028, 872, 562]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [glossary](/reference/glossary/),
  [architecture glossary](/architecture/glossary/), [docs with everything](/contributing/docs-with-everything/)
- **Decision:** (#2029.) The product vocabulary is a Go-owned table, `internal/vocab/terms.yaml`,
  embedded in the `vocab` package and validated on load (one claim per word, collection, schema,
  nav path and verdict; a definition is one sentence; a retired word carries its pattern and the
  decision that retired it).
  - `make gen` runs `cmd/vocabgen`, which writes `reference/glossary.md`: operator terms, then
    admin terms, each with its API collection, permission noun and the words it replaced.
  - The entity description is set by a **post-pass**: `describeEntitySchemas` runs last in
    `registerRoutes` and writes each mapped schema's description into Huma's component registry,
    so the spec, the generated client and the CLI help all carry the term's definition. Huma's own
    mechanism, a `doc` tag or a `TransformSchema` method on each body struct, was rejected: it
    puts the definition in Go beside the struct, a second copy of the table's sentence that
    drifts, and scatters one table across dozens of files. A schema the table
    names that the spec lacks fails the API guard, so the post-pass cannot silently miss.
  - Retired words (estate, interface, display name, and now inventory and workspace) are rows on
    the term that replaced them; the docslint denylist reads them from the table, keeping only
    retired code identifiers in its own list.
  - Guards: every top-level API collection, every console nav entry and every health verdict maps
    to a term or to a listed `not_term` with a reason; a nav label is its term's word; each term's
    permission noun is one a route enforces.
- **Context:** the taxonomy decisions under epic #2028 settled the words; this makes them hold.
  The architecture glossary had grown to carry internal mechanism beside product nouns and was
  hand-maintained, so a rename reached it late or not at all, and the nav hints still said
  workspace (#872) and inventory. The architecture glossary is frozen as the internal reference
  with a banner; per-field descriptions stay with #562.
