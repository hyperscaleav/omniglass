---
title: "Simulated devices can be broken on purpose (#885 rollup 2, slice #889)"
date: 2026-10-18
refs: [885, 889]
---

An operator unplugs, slows, or degrades a simulated device through `PUT /components/{name}/impairment`
(`simulation:update`, audited), and the simulator beside its node, started with `--impairments` and
`NET_ADMIN`, applies it through netlink: an nftables table drops traffic to unplugged devices and
ICMP echoes to loopback addresses that are no device (so ping tells the truth), and a netem class per
degraded device adds its delay, jitter, loss and rate. The release image is distroless, so no `nft`
or `tc` binary is involved. The real applier and the whole loop are tested in a `NET_ADMIN`
container (`make test-netadmin`, part of `make test` and CI), never on the host: unplugging fails the
collector's ping and TCP checks and both endpoints read down, replugging recovers, a 300 ms delay
shows in the connect time, and a rate limit holds both ways (the review caught it shaping only
the requests; a second, rate-only class now takes the device's replies, so the latency still counts
once). Two findings came out of it. Impairments are opt-in, because a simulator
run as root on a workstation would otherwise rewrite the host's firewall. And an unplugged device's
system verdict does not move: a component's condition is its active alarms, and nothing raises one
from reachability yet (#819).
