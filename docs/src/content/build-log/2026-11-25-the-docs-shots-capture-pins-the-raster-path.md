---
title: "The docs-shots capture pins the raster path (#1860)"
date: 2026-11-25
refs: [1860, 774]
---

The capture browser runs with `--disable-skia-runtime-opts`, so Skia should no longer pick a SIMD path from the host CPU.
The byte-identical result across AVX2 and non-AVX2 hosts is not yet verified; that comparison is still owed on #1860. `capture.sh` logs the CPU model and the AVX2 and AVX-512 flags. The baselines
committed before the pin are rewritten by the next recapture.
