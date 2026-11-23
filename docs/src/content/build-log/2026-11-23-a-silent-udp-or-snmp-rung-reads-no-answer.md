---
title: "A silent udp or snmp rung reads no answer, not down (#2296)"
date: 2026-11-23
refs: [2296]
---

Over a datagram transport silence proves nothing, yet a timed-out udp or snmp rung read "down". The
ladder hint, the component leaf's collection badge and the endpoint pill now read "no answer" and
name the causes (host off, agent stopped, wrong v2c community, filtered). Copy only: the reason
value, the verdict and the gate's unreachable streak are unchanged, and a tcp timeout still reads down.
