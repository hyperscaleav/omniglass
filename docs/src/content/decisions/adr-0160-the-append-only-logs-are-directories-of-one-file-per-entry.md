---
title: "The append-only logs are directories of one file per entry, spliced into their pages"
date: 2026-10-24
status: "Accepted"
summary: "The build log, the decision log and the adversarial-review anti-pattern catalog keep one file per entry (`docs/src/content/build-log/<YYYY-MM-DD>-<slug>.md`, `docs/src/content/decisions/adr-NNNN-<slug>.md`, `references/anti-patterns/<id>.md`) instead of one shared file every PR appends to (#1095, the architect's request). The docs build splices the entries into the page where a directive sits (`::build-log`, `::decision-index`, `::decision-log`), before the parse, so the page renders as one document; the decision index is generated from the files' frontmatter. ADR numbers stay sequential: a collision fails the decisions lint naming both files, and `make adr-renumber` moves one and rewrites every reference. The generated OpenAPI JSON is written pretty-printed with sorted keys"
---

- **Date:** 2026-10-24 | **Status:** Accepted | **Pages:** [docs with everything](/contributing/docs-with-everything/),
  [build log](/architecture/build-log/), [decision log](/architecture/decisions/)
- **Decision:** An append-only log is a **fragment directory**: one file per entry, frontmatter for
  the entry's metadata (a small line grammar both the docs build and `internal/docslint` read, not
  YAML), and a page that holds no entries but a directive line where the docs build splices them in.
  The build log, the decision log, and the anti-pattern catalog all take the pattern. The splice
  happens on the page's text before Astro parses it (a parser wrap in `remark-fragments.mjs`), not as
  a tree transform, so heading anchors dedupe across the whole page, the table of contents lists every
  entry, and smartypants and lists render exactly as they did inline; the docs loader keys a host
  page's cached render on its fragments so a restored cache or `pnpm dev` never serves a stale log.
  ADR numbers stay sequential and stable (`ADR-0001` onward, anchors unchanged); two PRs that take
  the same next number fail `TestDecisionsFormat` naming both files, and the one landing second
  runs `make adr-renumber FROM=NNNN TO=MMMM`, adding `FILE=` once both decisions are in the tree so
  that git blame scopes the rewrite to its own lines. Git no longer conflicts on a collision, and
  main does not require a PR to be up to date, so a collision can reach main; the tool repairs it
  there too, and a required up-to-date check would prevent it. `api/openapi.json` is written
  pretty-printed with sorted keys, and `.gitattributes` marks the generated artifacts.
- **Context:** In the thirty days to 2026-10-24, the build log was touched by 39 of 66 commits on
  main, the decision log by 21, the catalog by 8, and the one-line `openapi.json` by 21: every PR
  appended at the same place, so any two open PRs conflicted, and concurrent ADRs took the same
  number and were renumbered by hand, references and all. One file per entry removes the shared
  line; generating the index removes the second place an ADR had to be written (and fixed an index
  whose blank lines had cut the table at ADR-0095 since #679). A custom merge driver was rejected
  because GitHub's server-side merge ignores it. The entries that predate the split keep their
  logged order under a four-digit sequence prefix (`0001-` to `0398-`) rather than a date, since
  their landing dates do not run in log order.
