---
title: "The logs and the generated spec stop conflicting on every merge (#1095)"
date: 2026-10-24
refs: [1095]
---

The build log, the decision log, and the adversarial-review anti-pattern catalog are fragment
directories now ([ADR-0162](/architecture/decisions/#adr-0162-the-append-only-logs-are-directories-of-one-file-per-entry-spliced-into-their-pages)):
398 build-log entries, 159 decisions, and 27 anti-patterns became one file each, split by a one-time
script that proved the files rebuild the old text byte for byte. The pages carry a directive
(`::build-log`, `::decision-index`, `::decision-log`) that `docs/src/plugins/remark-fragments.mjs`
expands before the parse, from the shared renderer in `fragments.mjs`; the docs loader
(`fragments-loader.mjs`) folds a host page's fragments into its cache digest and, in dev, re-announces
the page when one changes; `scripts/check-fragments.mjs` renders every directory before `astro build`
(refusing a directive line in an entry body, the one way an entry could fail a remark plugin), since a
render error on a `.md` page is swallowed, and `scripts/check-fragment-pages.mjs` proves afterwards that
every index anchor names a heading on the built decision log and the build log carries a heading per
section; and the full-text `llms-full.txt` expands the same directives. The build-log page renders byte-identical HTML. The decision log's entries render
byte-identical, and its index, generated from the files, is one table again: blank lines had cut it
after ADR-0095 since #679, leaving 64 rows as pipe text, and the renderer now escapes a bare `|` in a
cell, which would otherwise have cut ADR-0102's summary short.

The docs lint follows the files: `TestDecisionsFormat` reads the directory (the index fields, Date and
Status in the body, one file per number naming both files on a collision, no gap), `TestBuildLogFormat`
and `TestAntiPatternCatalog` check the entry shapes, `TestLogsStayFragments` fails an entry written into
a page or the catalog's old single file, and `walkDocs` reads the entry files so the path, table, and
env lints keep their coverage of the history. `make adr-renumber` (`cmd/adrrenumber`) renames a decision
and rewrites every `ADR-NNNN` mention, `#adr-NNNN-` anchor, and file name in any text file (docs, code
comments, `.proto`, skills), refusing a taken number; with `FILE=` it repairs a tree that already holds both
decisions, rewriting only the lines git blame gives to the commits after `SINCE`. `api/openapi.json` is written pretty-printed with
sorted keys (`canonicalJSON`), semantically identical, with the typed client, the CLI tree, and the
settings schema regenerating unchanged, and `.gitattributes` marks the generated artifacts. Proven
test-first: the renderer's node tests (ordering, the two entry forms, the index anchors and escaping, the
directive splice, a collision), the Go lints and the renumber tool over fixture trees, and the canonical
JSON writer. Every procedure that wrote the old files now teaches the new one (`/record-decision`,
`/ship-slice`, `/run-feature-loop`, `/autopilot`, CLAUDE.md, docs with everything).
