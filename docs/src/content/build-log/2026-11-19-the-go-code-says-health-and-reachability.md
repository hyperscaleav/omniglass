---
title: "The Go code says health and reachability, not verdict (#2243)"
date: 2026-11-19
refs: [2086, 2102, 2243]
---

ADR-2086 (#2086) retired verdict as the word for health's result. The Go identifiers follow (the
first half of #2102): `health.Verdict` is now `health.Value`, and the helpers it names
(`SystemVerdict`, `SystemVerdictWith`, `ComponentVerdict`, `ImpactVerdict`, `ParseVerdict`)
follow. The storage reads that returned a verdict (`SystemVerdicts`, `ComponentVerdict`,
`locationVerdict`, `recordedVerdict`) take health; the reach helpers an endpoint publishes
(`EndpointVerdict`, `ladderVerdict`, `probeVerdict`, `AuthVerdict`) take reachability, and
`internal/collection/verdict.go` is `internal/collection/endpoint_reachability.go` to match.
`SettlementVerdict` is now `Settlement`. The API is unchanged: the body types in
`internal/api` (`SystemVerdictBody`, `ReachVerdictBody`, their fields and JSON tags) stay as
they are, because they generate `api/openapi.yaml`, and `make gen` holds the line. The soak,
perf and footprint gates, a subscription's test and a target's test send keep verdict: those
are judgements about a run, not about health. The API half of #2102 follows in #2244.
