---
title: "A held snmp or udp rung reads responsive up, not down (#2294)"
date: 2026-11-23
refs: [2294, 1650]
---

An snmp endpoint refused with `authorizationError` read responsive up on the first poll and then
responsive down (`rejected: auth`) on every held poll, though the refusal was a reply that proved the
agent there; udp did the same, including on the terminal reply itself. A held rung now reads responsive
up with the reason `held: <class> since <ts>`, and a udp terminal reply reads up, responded. The gate's
`probeReachability` treats a held reading as no verdict, so the #1650 property holds: a held rung
neither adds to nor resets the unreachable streak. A timeout is unchanged. Regression tests:
`TestAUDPTerminalReplyGatesTheEndpoint`, the held assertions in the snmp rung test, and
`TestProbeVerdictDropsOnlyARefusalsEcho`.
