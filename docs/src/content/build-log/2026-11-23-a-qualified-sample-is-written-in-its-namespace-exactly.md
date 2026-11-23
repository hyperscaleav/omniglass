---
title: "A qualified sample is written in its namespace exactly (#1457)"
date: 2026-11-23
refs: [1457, 1433, 318]
---

A push naming `local/K`, where `local/K` is also the row the bare name `K` reaches, used to be
handed to the Storage Gateway bare, so the event and calc rules reading `K` would still judge it. The
consumer's catalog is a kept snapshot (ADR-1279), though, and the Gateway re-resolved the bare name
at write time: had the operator deleted `local/K` in between, the sample landed on `og/K`.

The consumer now always passes the namespace a sample names, and says separately, with
`BareInEffect`, that the row is the one the bare name reaches. The Gateway reads the type in the
named namespace exactly, so a deleted row refuses the sample, and the rule marks, the event rule
evaluator and command settlement read the flag rather than "no namespace". Regression tests:
`TestDerivedSamplesCarryTheirNamespace` (`internal/bus`) and
`TestAQualifiedSampleOfTheBareRowIsJudgedAndNeverFallsBackToOg` (`internal/storage`).
