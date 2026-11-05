---
title: "The lab keeps a generic SNMP device (#1372)"
date: 2026-11-05
refs: [1372, 1363]
---

Moving the Boreal Edge displays onto `boreal-edge-snmp` left `snmp-generic` attached to no simulated
device, which the lab's method coverage and the SNMP e2e both hold. Rather than list the generic driver
as a gap, the lab gains an unclassified device: `auditorium-unclassified` on the existing
`generic-device` product, with a `generic-device` simulator profile that serves MIB-II `sysDescr` and
`sysUpTime`, the two objects `snmp-generic` reads, so it lands `model-number` and `uptime` and nothing
more. The `generic-device` product now names `snmp-generic` as its driver, so the lab plan attaches it.

It sits in the auditorium system as a member filling no role, outside the standard's slots, beside the
house spare microphone: the devseed already models that case, and a custom standalone system would have
added a system to the fleet for one box.
