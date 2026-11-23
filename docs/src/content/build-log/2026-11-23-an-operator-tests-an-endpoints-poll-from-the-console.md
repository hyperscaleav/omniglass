---
title: "An operator tests an endpoint's poll from the console (#2427)"
date: 2026-11-23
refs: [2427]
---

`run-endpoint-poll` had no console caller: adding a device ended with waiting for the next
collection cycle and hunting for samples. A `PollTest` component now offers a Test button per poll
(and Test all) on the endpoint's blade and under each endpoint in the component's Collection
section, gated on `endpoint:run`, and draws the run in place: the samples read with value and
unit, the round trip and the outpost, or the refusal with its real cause (a gate's own reason, no
outpost, a failure, a timeout). The walkthrough `test-an-endpoints-poll` records it, and the
[operator page](/operator/collection/#test-a-poll-now) teaches it.
