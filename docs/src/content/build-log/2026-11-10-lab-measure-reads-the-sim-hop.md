---
title: "Lab measure reads the sim hop of the loss table (#1694)"
date: 2026-11-10
refs: [1694, 1641, 1639, 1738]
---

The delivery table that `lab measure` prints now opens with the sim hop: one row per protocol the
nodes tally, the requests the simulated endpoints settled against the replies that reached the node,
read from the closed loop's marks by `closedloop.SimHop`. A healthy lab reads 0; a simulator that
withholds, cannot write, or loses its replies reads each as its own cause (`withheld`, `dropped`,
`not-returned`), while a refusal that came back counts as returned. End to end composes the sim
hop's pooled loss into each kind. The rows are counted in requests because a simulator cannot know
which kinds an answer carried. A real-socket test drives a simulated SNMP agent that withholds
answers to a wrong community and reads them as sim-hop loss, and the scale lab tests require every
sim row at 0. Rendering the closed loop's batch gaps as ratios is #1738. The decisions are
[ADR-1694](/architecture/decisions/#adr-1694-the-sim-hop-is-read-per-protocol-in-requests-from-the-closed-loop).
