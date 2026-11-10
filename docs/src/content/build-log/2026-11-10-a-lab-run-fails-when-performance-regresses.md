---
title: "A lab run fails when performance regresses past the committed baseline (#1640)"
date: 2026-11-10
refs: [1640, 1632, 1639, 1641, 1695, 1719, 1625, 1751]
---

`make perf-check SIZE=ci|laptop` (`omniglass lab perf check`) brings up three throwaway scale labs of
a named size one after another (corporate at 300 systems for `ci`, 1000 for `laptop`), warms each for
2 minutes, measures a 5 minute window with `lab measure`'s own `MeasureScale`, and compares the runs
with the size's committed baseline through one pure function, `ComparePerf`. Exact figures (closed-loop
gaps, ingest lag not measured, redeliveries, delivery loss per hop and kind) must be 0; counts that do
not depend on the machine gate on any host; times and usage gate only on the host the baseline's
fingerprint names, and read reported elsewhere. A figure fails only past its tolerance and past the
runs' and the baseline's combined spread; runs spread past the tolerance read noisy. A run of another
size, window, warm-up or device set, a restarted node or an active impairment is refused as invalid
before any figure is read. `make perf-baseline` generates a baseline from the same runs and refuses
one that is noisy or nonzero on an exact figure; `--append` keeps each run as a #1632 series line,
and `--samples` reads a series' last runs in place of fresh labs. The class tables render into the lab
guide from `make gen`, and #1695's 3000 size extends them by a row. The `perf` workflow runs the
check on the opt-in `run:perf` label, never as a required check, and its dispatch regenerates the `ci`
baseline as an artifact. A lab's fleet size no longer counts the platform system the boot seed plants
(#1625), so a lab at 20 systems reads 20 and the gate does not refuse its own baseline as another size.
No baseline file is committed yet: this slice ran on a shared build host
whose fingerprint gates neither class, so the `ci` baseline comes from that dispatch and the `laptop`
one from the architect's laptop (#1751). The decision is
[ADR-1640](/architecture/decisions/#adr-1640-a-scale-lab-measured-end-to-end-gates-against-a-committed-baseline).
