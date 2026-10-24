---
name: record-decision
description: "Use when recording an architecture decision (an ADR) in the decision log: numbers it by the issue it was decided under, writes it as its own file under docs/src/content/decisions/ with the index-row fields and the required Date/Status/Pages body, updates the pages the decision touches, appends any retired vocabulary to the internal/docslint denylist, and renumbers with make adr-renumber in the rare clash of two suffixed decisions on one issue. Invoke for every divergence note, reversal, or vocabulary retirement."
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

1. **Number it by its issue.** A decision takes the number of the issue it was decided under
   (#1108): a decision made while working issue #NNNN is ADR-NNNN. Issue numbers never
   collide, so there is nothing to check on main. The second decision on the same issue takes
   a suffix, ADR-NNNN.2, then ADR-NNNN.3. ADR-0001 to ADR-0163 are the closed legacy range,
   numbered in sequence before the rule; never add to it, and never number a new decision
   below 0164 (a decision on an older issue is filed under a new one).
2. **Write the file** `docs/src/content/decisions/adr-NNNN-<slug>.md` (the number zero-padded
   to four digits, so issue #987 is `adr-0987-...` and #12345 is `adr-12345-...`; a suffix
   is `adr-NNNN.2-<slug>.md`), the slug a short lowercase-hyphen form of the title:

   ```markdown
   ---
   title: "<the decision, one line, head-noun form>"
   date: YYYY-MM-DD
   status: Accepted
   summary: "<the index row's Decision cell: the decision in a sentence or two>"
   refs: [NNNN]
   ---

   - **Date:** YYYY-MM-DD | **Status:** Accepted | **Pages:** [page](/architecture/page/), ...
   - **Decision:** what was decided, in one or two lines.
   - **Context:** what forced it, and the why.
   ```

   `refs` must include the issue the number came from (other issue and PR numbers may follow).
   The frontmatter is the index row: `title` becomes the `### ADR-NNNN: <title>` heading
   (and its `#adr-nnnn-...` anchor; a suffix's dot drops out, so ADR-NNNN.2 anchors as
   `#adr-nnnn2-...`), `date`, `status` and `summary` its cells; the index lists the legacy
   range and the issue numbers in number order. Values are a
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
   must be green: the frontmatter fields present, the number among the refs, Date and Status
   in the body, one file per number, suffixes running from .2, no entry written into the page.
   `cd docs && pnpm build` renders it.

## When two PRs both add a second decision to one issue

Issue numbers cannot collide, but two PRs on one issue can each add its next suffix (both take
ADR-NNNN.2). The two files have different names, so git merges them without a conflict; the
decisions lint names both. On the branch whose decision lands second:

- **Before merging main**, the number means only your decision, so every reference moves:

  ```bash
  make adr-renumber FROM=NNNN.2 TO=NNNN.3
  ```

- **After merging main** (both files are in the tree), name your file; only the lines your
  branch's commits wrote (blamed to `origin/main..HEAD`, or not yet committed) are rewritten:

  ```bash
  make adr-renumber FROM=NNNN.2 TO=NNNN.3 FILE=adr-NNNN.2-<your-slug>.md
  ```

- **If both landed on main**, repair it on a fix branch, naming the later decision and setting
  SINCE to the commit before its PR landed:

  ```bash
  make adr-renumber FROM=NNNN.2 TO=NNNN.3 FILE=adr-NNNN.2-<later-slug>.md SINCE=<the commit before it landed>
  ```

The tool rewrites mentions and file names in the dot form (`ADR-NNNN.2`, `adr-NNNN.2-<slug>.md`)
and anchors in the dotless one (`#adr-NNNN2-`), and leaves the plain `ADR-NNNN` alone. Stage the
renamed file (`git add -A docs/src/content/decisions/`), review `git diff`, and run
`go test ./internal/docslint/ -run TestDecisionsFormat -count=1`. It rewrites every mention of the
number in the lines it touches, so write examples in prose with placeholders (`ADR-NNNN`), never a
real number.
