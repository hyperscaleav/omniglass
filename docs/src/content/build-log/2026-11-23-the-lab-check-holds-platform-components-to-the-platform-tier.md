---
title: "The lab check holds platform components to the platform tier (#2553)"
date: 2026-11-23
refs: [2553, 1910, 1678]
---

`make test-lab` failed on main because `labcheck.Evaluate` held the platform's own components
(`server`, `edge-hq`) to the endpoint-driven built-in and collected tiers, yet they have no
endpoint by design and report platform metrics themselves. A platform component (product
`omniglass-server`, `omniglass-node` or `omniglass-outpost`) with no endpoint is now held to the
platform tier it carries: it must report at least one platform-class value. A fleet component with
no endpoint still fails. Regression test:
`TestAPlatformComponentWithNoEndpointIsNotAFindingButAFleetOneIs` in `internal/labcheck`. Not run
here: `make test-lab` on a real daemon (no lab in this run).
