---
title: "The reachability demo DSP reports its mute and level (#1507)"
date: 2026-11-05
refs: [1507]
---

With the simulated fleet, the seed turns the worked reachability check's boardroom DSP into a
simulated `polaris-dsp-16` and re-aims its HTTP and line-protocol checks at a simulated address. It
never gained the product's driver, though: the fleet plan skips any component that already has an
endpoint, and the demo had its two reach checks. So the one DSP the lab's DSP floor found without
`audio-level` and `mute` was the demo, and `make test-lab` failed on it. The re-aim now also attaches
`polaris-dsp-snmp` at the same address, once: a later run finds the demo already off its fixed
address and changes nothing.
