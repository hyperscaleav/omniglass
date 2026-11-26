---
title: "The docs-shots capture pins the raster path (#1860)"
date: 2026-11-25
refs: [1860, 774]
---

The capture browser runs with `--disable-skia-runtime-opts`, so Skia no longer picks a SIMD path from the host CPU.
`capture.sh` logs the CPU model and the AVX2 and AVX-512 flags. The baselines committed before the pin are rewritten by
the next recapture.

Measured on an AVX2 host and a no-AVX host, the same tree with and without the flag
([#1860](https://github.com/hyperscaleav/omniglass/issues/1860#issuecomment-7169963900)): `users.png` was
byte-identical across both CPUs with and without the flag, so the five-pixel avatar difference that opened the issue did
not reproduce on the current image. `notification-target-new` (8 pixels) did differ across the CPUs without the flag and is
byte-identical with it, which is the raster difference the pin removes. 36 of the 37 baselines match across the two CPUs
with the flag; `fleet-history` does not, by the position of its alarm marker, a lab timing difference the pin does not
reach.
