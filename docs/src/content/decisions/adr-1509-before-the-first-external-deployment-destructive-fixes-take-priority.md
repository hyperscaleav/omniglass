---
title: "Before the first external deployment, destructive fixes take priority over back-compat"
date: 2026-11-05
status: "Accepted"
summary: "Established stance on #1509. Omniglass has no external deployment, no installed base, and no operator data outside dev, CI and the lab. Until the first external deployment, destructive fixes (renaming seeded keys or fixtures, reshaping schema, removing or renaming an API field, deleting a compatibility shim) take priority over back-compat paths, deprecation periods, or data-migration machinery. This rule does not change migration mechanics: applied migrations are never edited, and schema changes are always expressed as new migrations. This section will be removed in the PR that prepares the first external deployment."
refs: [1509, 1371]
---

- **Date:** 2026-11-05 | **Status:** Accepted | **Pages:** [contributing](/contributing/slice-workflow/)
- **Decision:** (#1509.)
  1. **Omniglass is pre-deployment.** There is no external deployment, no installed base, and no operator data outside dev, CI, and the lab. This stance applies only before the first external deployment.
  2. **Destructive fixes are correct.** When the correct fix is destructive (renaming seeded keys or fixtures, reshaping schema, removing or renaming an API field, deleting a compatibility shim), agents should make that fix directly instead of adding back-compat paths, deprecation periods, or data-migration machinery for data that does not exist.
  3. **Migration mechanics are unchanged.** Applied migrations are never edited, and schema changes are always expressed as new migrations (DDL idempotence). This rule is not relaxed.
  4. **Exit condition.** This stance will be removed in the PR that prepares the first external deployment.
- **Context:** Found during #1371, where an agent weighed a coarse workaround against renaming seeded systems. Without an installed base, workarounds are pure cost: they complicate the code, constrain future changes, and add nothing to the user experience. A destructive fix is the simplest, clearest path.
- **Not chosen:**
  - **Back-compat shims and deprecation periods.** They add cost and complexity during pre-deployment, when every change is made quickly and rarely affects real data.
  - **Data-migration machinery for non-existent data.** It adds test burden and operational machinery that will be replaced on the first real migration anyway.
