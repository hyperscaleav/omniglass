---
title: "Bare endpoint params: timeout and count validated at write (#980)"
date: 2026-11-19
refs: [980, 1187]
---

`checkBareParams` now validates two additional fields that were documented but not checked at write
time: `timeout` must be a positive Go duration string (e.g. `"30s"`, `"1m30s"`) when present on any
dialed transport or on udp, and `count` must be a non-negative integer when present on an `icmp` endpoint.
Both are refused with a 422 on create (a 400 on an edit) naming the field. Before, an invalid
value like `{"target":"10.0.0.1","timeout":"five minutes"}` stored 201 and the outpost then
failed to parse the duration at probe time, skipping the task forever. The outpost's own parse
(probe.go: `time.ParseDuration` for timeout, `int` unmarshal for count) is the reference for
what parses; the write check is stricter on timeout and also refuses zero or negative
durations, which would otherwise store an endpoint with no usable timeout.
