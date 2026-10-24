---
title: "Look-alike findings are held, not filed (#966)"
date: 2026-10-21
refs: [966]
---

Keys deduplicate the same defect seen again, but not one a person filed in other words or two
lenses described under two keys. `possibleDuplicates` compares a finding's title with every open
issue's (prefixes, stop words and plurals folded; a match shares at least two words and 60% of the
shorter title), and the filing plan holds a match as `possible-duplicate` with its candidates until
the run adopts one or confirms the finding with `--confirm-new`; look-alikes within one run fold into
the more severe. A sighting posts once per issue per day. The supervised trial's findings were filed
through it as the program's first run (epic #978, bugs #979 to #981 and #983): the guard held one
suggestion against two unrelated issues, which the run read and confirmed, and a repeat dry run
planned nothing new.
