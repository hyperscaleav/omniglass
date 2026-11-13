---
title: "A perf size is named by its rung"
date: 2026-11-11
status: "Accepted"
summary: "Amends ADR-1640: the perf gate's sizes are named by the systems they measure, `300` and `1000`, not by the host each was meant to run on (`ci`, `laptop`), and a baseline file is `<scenario>-<systems>.json`. Where a rung runs is a host fact the baseline's fingerprint already records; the 1000 rung moves from the architect's laptop to the Proxmox worker"
refs: [1780, 1640, 1737, 1751]
---

- **Date:** 2026-11-11 | **Status:** Accepted | **Pages:** [lab](/guides/lab/#checking-against-the-baseline), [test-driven](/contributing/test-driven/), [labels](/contributing/labels/)
- **Decision:** (#1780; amends [ADR-1640](#adr-1640-a-scale-lab-measured-end-to-end-gates-against-a-committed-baseline).) `--size` and `make perf-check SIZE=` take a rung of the ladder, `300` or `1000`, and the committed baseline for a rung is `corporate-<systems>.json`. A size still fixes the scenario, systems, warm-up and window; it no longer names a host.
- **Consequences:** The committed `corporate-300.ci.json` becomes `corporate-300.json` with its `size` label changed from `ci` to `300` and every figure byte for byte the same (the baseline decodes strictly, so the label moves with the name). The 1000 baseline is generated on the Proxmox worker (#1737), not a laptop. A new rung is a new size entry, named by its systems.
- **Context:** The laptop baseline for the 1000 size refused on a figure whose spread was measurement drift (#1779), and the architect ruled that the 1000 rung runs on Proxmox and that the gate should be organized by the sizing ladder rather than by the machine a run happens on. A host-named size goes stale the moment the host changes; the host is already pinned by the baseline's fingerprint, which gates the host-dependent figures.
- **Not chosen:**
  - **Rename `laptop` to `proxmox`.** It repeats the mistake: the next host change renames the size again, and the name still says nothing about what is measured.
  - **Keep host names and add rung aliases.** Two names for one size, for a gate with no installed base, is a compatibility path for data that does not exist.
