---
name: record-decision
description: "Use when recording an architecture decision (an ADR) in the decision log: assigns the next free number, writes the entry with the required Date/Status/Pages fields, adds the index-table row, updates the pages the decision touches, and appends any retired vocabulary to the internal/docslint denylist. Invoke for every divergence note, reversal, or vocabulary retirement."
---

# Record a decision

Every reversal, settled open question, divergence from a page's present-tense design, and
vocabulary retirement gets one entry in the decision log
(`docs/src/content/docs/architecture/decisions.md`), landed in the same PR as the change it
records. The log is append-only: entries are never edited away, deleted, or renumbered.

## Procedure

1. **Take the next number.** Find the highest existing `### ADR-NNNN` heading in
   `decisions.md` and use the next number. Never reuse a number and never renumber an
   existing entry, even to close a gap; the by-hand alternative is how a duplicate ADR
   shipped once already.
2. **Write the entry** at the end of the log, matching the house shape:

   ```markdown
   ### ADR-NNNN: <the decision, one line, head-noun form>

   - **Date:** YYYY-MM-DD | **Status:** Accepted | **Pages:** [page](/architecture/page/), ...
   - **Decision:** what was decided, in one or two lines.
   - **Context:** what forced it, and the why.
   ```

   `Date`, `Status`, and `Pages` are required fields; the decisions-format lint fails
   without them. `Status` is one of `Proposed`, `Accepted`, `Resolved`, `Superseded`. A
   superseded entry stays in place, marked `Superseded by [ADR-MMMM](#...)`.
3. **Add the index row.** The index table near the top gets one row per entry:
   `| [ADR-NNNN](#adr-nnnn-<anchor-from-the-heading>) | date | status | decision |`.
4. **Update the pages the decision touches.** Each page named in `Pages` carries the change
   inline: the page says what is true now, the log says why and when. A log entry with no
   page edit is half a decision.
5. **Retired an identifier?** Append a `BannedTerm` (pattern, replacement, origin
   `ADR-NNNN`) to the denylist in `internal/docslint/docslint.go` **in the same PR**, sweep
   the docs for the term, and re-run `go test ./internal/docslint/`. A retirement without
   its denylist entry is the drift class the lint exists to prevent.
6. **Finish with the lint.** `go test ./internal/docslint/ -run TestDecisionsFormat -count=1`
   must be green: unique numbers, numeric order, an index row per entry, the required
   fields present.
