---
title: "Builders self-review before every push (#2600)"
date: 2026-11-25
refs: [2600]
---

The agent-build prompt names the self-review in order: `/adversarial-review` on the full diff, the neighbor
check, `make preflight`, then `gh pr ready`. agent-fix runs `/adversarial-review` on the commits of its own run
plus the neighbor check before its one push. `/ship-slice` step 5 and both prompts extend the neighbor check to
every UI label, term, flag, route or identifier the diff added, renamed or removed, old and new spellings,
across the whole docs tree.
