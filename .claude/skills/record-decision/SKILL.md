---
name: record-decision
description: "Use when recording an architecture decision (an ADR) in the decision log: takes the next free number, writes the decision as its own file under docs/src/content/decisions/ with the index-row fields and the required Date/Status/Pages body, updates the pages the decision touches, appends any retired vocabulary to the internal/docslint denylist, and renumbers with make adr-renumber when another PR took the number first. Invoke for every divergence note, reversal, or vocabulary retirement."
---

# Record a decision

Every reversal, settled open question, divergence from a page's present-tense design, and
vocabulary retirement gets one entry in the
[decision log](../../../docs/src/content/docs/architecture/decisions.md), landed in the same PR
as the change it records. The log is append-only: entries are never edited away or deleted.

**Each decision is its own file** under `docs/src/content/decisions/`. The page holds no
entries: the docs build splices the index table and the entries in where `::decision-index`
and `::decision-log` sit (`docs/src/plugins/fragments.mjs`), so two PRs recording decisions
add two files and never conflict (#1095). Never write an entry or an index row into
`decisions.md`; `TestLogsStayFragments` fails it.

## Procedure

1. **Take the next number.** The highest `adr-NNNN-*.md` in `docs/src/content/decisions/`
   plus one, checked against a freshly fetched `origin/main`
   (`git fetch origin main && git ls-tree --name-only origin/main docs/src/content/decisions/ | tail -1`),
   since another open PR may land first. Numbers run from 0001 with no gap.
2. **Write the file** `docs/src/content/decisions/adr-NNNN-<slug>.md`, the slug a short
   lowercase-hyphen form of the title:

   ```markdown
   ---
   title: "<the decision, one line, head-noun form>"
   date: YYYY-MM-DD
   status: Accepted
   summary: "<the index row's Decision cell: the decision in a sentence or two>"
   ---

   - **Date:** YYYY-MM-DD | **Status:** Accepted | **Pages:** [page](/architecture/page/), ...
   - **Decision:** what was decided, in one or two lines.
   - **Context:** what forced it, and the why.
   ```

   The frontmatter is the index row: `title` becomes the `### ADR-NNNN: <title>` heading
   (and its `#adr-nnnn-...` anchor), `date`, `status` and `summary` its cells. Values are a
   double-quoted (JSON) string, a single-quoted string, or a bare word; a bare `|` in
   `status` or `summary` is escaped for the table by the build. The body's `Date`, `Status`,
   and `Pages` fields are required, and its `Date` must equal the frontmatter's. `Status` is
   one of `Proposed`, `Accepted`, `Resolved`, `Superseded`; a superseded entry stays in
   place, its `status` reading `Superseded by [ADR-MMMM](#adr-mmmm-...)`. The body carries
   no headings of its own.
3. **Update the pages the decision touches.** Each page named in `Pages` carries the change
   inline: the page says what is true now, the log says why and when. A log entry with no
   page edit is half a decision. Amending an earlier decision edits that decision's own file
   (an `**Amended (#NNN):**` note), never a new copy.
4. **Retired an identifier?** Append a `BannedTerm` (pattern, replacement, origin
   `ADR-NNNN`) to the denylist in `internal/docslint/docslint.go` **in the same PR**, sweep
   the docs for the term, and re-run `go test ./internal/docslint/`. A retirement without
   its denylist entry is the drift class the lint exists to prevent.
5. **Finish with the lint.** `go test ./internal/docslint/ -run 'TestDecisionsFormat|TestLogsStayFragments' -count=1`
   must be green: the frontmatter fields present, Date and Status in the body, one file per
   number, no gap, no entry written into the page. `cd docs && pnpm build` renders it.

## When another PR took the number

Two open PRs can take the same next number. Whichever merges second sees
`TestDecisionsFormat` name both files once main is merged in (the PR's CI runs on the merge
with main). On that branch, **before merging main**, while the number still means only your
decision:

```bash
git fetch origin main
make adr-renumber FROM=NNNN TO=MMMM   # renames your file, rewrites every ADR-NNNN mention and #adr-NNNN- anchor
git add -A docs/src/content/decisions/  # the renamed file is new to git
git commit -am "Renumber my decision, whose number main took first"
git merge origin/main
```

`NNNN` is the number both PRs took and `MMMM` the next free one. The tool rewrites references
across docs, code comments, and skills, and refuses when the target number is taken or when two
files already hold the source number (after a merge, the references can no longer be told apart:
abort the merge, renumber, merge again). It rewrites every mention of the number, so write
examples in prose with placeholders (`ADR-NNNN`), never a real next number.
