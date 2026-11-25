---
title: "The docs-shots capture pins the raster path (#1860)"
date: 2026-11-25
refs: [1860, 774]
---

The capture browser runs with `--disable-skia-runtime-opts`, so Skia no longer picks a SIMD path from the host CPU and a
baseline is the same bytes on any host. `capture.sh` logs the CPU model and the AVX2 and AVX-512 flags. The baselines
committed before the pin are rewritten by the next recapture.
