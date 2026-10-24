---
title: "An ADR's number is the number of the issue it was decided under"
date: 2026-10-24
status: "Accepted"
summary: "ADR numbers stop being sequential after ADR-0163 (#1108, the architect's ruling): ADR-0001 to ADR-0163 stay a closed, gap-free legacy range, and every later decision takes the number of the issue it was decided under, named in its frontmatter `refs` (this one is ADR-1108). A second decision on one issue takes a suffix from .2 (ADR-NNNN.2), whose anchor drops the dot (`#adr-NNNN2-...`). The index orders both ranges by value; the lint keeps one file per number and one anchor per decision, and `make adr-renumber` survives only for two PRs adding one issue's next suffix"
refs: [1108, 1095]
---

- **Date:** 2026-10-24 | **Status:** Accepted | **Pages:** [decision log](/architecture/decisions/),
  [docs with everything](/contributing/docs-with-everything/)
- **Decision:** A decision's number is the number of the issue it was decided under: a decision
  made on issue #NNNN is ADR-NNNN, its file `adr-NNNN-<slug>.md` (zero-padded to four digits) and
  its `refs` naming NNNN, which `TestDecisionsFormat` and the docs build both enforce. A second
  decision on the same issue takes a suffix, ADR-NNNN.2, then .3; the heading renders the dot and
  its slug drops it, so the anchor is `#adr-NNNN2-<slug>`, and the lint refuses two decisions that
  would share an anchor prefix (an issue whose number spells another's suffix). ADR-0001 to ADR-0163
  keep their numbers as a closed legacy range that runs with no gap; no new decision is numbered
  inside it, and a suffix there is refused. The index lists every decision in number order, the
  legacy range first. `make adr-renumber` takes suffixed numbers and is kept for the one clash left.
- **Context:** Fragment files ([ADR-0162](#adr-0162-the-append-only-logs-are-directories-of-one-file-per-entry-spliced-into-their-pages))
  ended the merge conflicts on the decision log, but sequential numbers still made every two
  concurrent decisions race for the same next one, and with no conflict to stop it a collision
  could land on main before anyone saw it (#1097 and #1098 took 0160 and 0161 while ADR-0162 was in
  review). An issue number is unique by construction and already known when the decision is made,
  so the race cannot start; the only shared space left is one issue's suffixes, which only PRs on
  the same issue can reach.
