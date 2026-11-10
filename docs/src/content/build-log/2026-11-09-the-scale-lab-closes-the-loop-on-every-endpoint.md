---
title: "The scale lab confirms every request reached its simulated endpoint and was answered (#1639)"
date: 2026-11-09
refs: [1639, 1640, 1661, 1662, 1663, 1664]
---

Every simulated endpoint now counts what reached it (connections, requests, streams) and how each
request ended (`answered`, `refused`, `error`, `dropped`, `withheld`), with a service-time
histogram and an in-flight high-water per protocol; every node counts the same units where its
bytes leave, and its spool counts the telemetry batches the bus acknowledged or dropped.
`GET /nodes/{name}/simulation/observation` reads the simulator, the node and the simulator again,
and `omniglass lab measure` takes that bracket at a window's start and end and reconciles it with
the node's telemetry receipts into seven named gaps, each a lower bound printed beside what could
mask it, plus an overlap ratio that flags any face sent two requests at once. A 20-system corporate
scale lab under `make test-lab` reads 0 lost on every gap and 0 overlap on every protocol. Session
traffic tallies (#1661), per-protocol impairment attribution (#1662), windowed unhit checks (#1663)
and the planted spool-drop run (#1664) are filed; #1640 gates on these gaps.
