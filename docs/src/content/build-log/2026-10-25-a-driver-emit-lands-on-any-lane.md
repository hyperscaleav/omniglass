---
title: "A driver emit lands on any lane, events and log lines included (#1145)"
date: 2026-10-25
refs: [1145, 1146, 1147]
---

The first part of #1145, the primitive the server ingress epic (#1146) and the files epic (#1147)
both consume ([ADR-1145](/architecture/decisions/#adr-1145-a-driver-emit-lands-on-any-of-the-four-lanes-and-a-log-lines-severity-is-rfc-5424s)).
A driver's emit now resolves against the event catalog beside the metric and property ones
(`internal/storage/driver_attach.go`), an event emit carries a message and attributes, and a log
line emit, which names no key, carries its message, severity, facility, source and attributes
(`internal/driver`). Any emit may read its time out of the payload in one of five layouts, clamped
to arrival past five minutes; a log line is never lost to an extract that misses, which faults
instead. `internal/logline` holds the one severity vocabulary (RFC 5424's eight levels and their
aliases) and the 16 KiB caps, and the Storage Gateway applies both to every `log_line` write.

On the node, an event rides the batch's event lane and a component's log line a new `task_logs`
field, never the self-log lane; a listener's `unmatched: log` lands what nothing claims, and a plain
poll fires its occurrences on change. Ingest files a task's lines in `log_line` on its component
with the collecting endpoint as their instance and the server's receipt beside their own time
(`received_at`). The component and system log reads filter by a severity threshold and a source and
list the window's sources; the console's component Activity tab carries the Logs panel beside the
events, with the filters, and the system workspace's Logs section takes them too.

The simulator's Newtron panel pushes its diagnostics log and its scene changes on the NVP face, and
a simulated route may declare named events, which the Kestrel bar's dial and disconnect routes use
to report a call starting and ending with its id. The shipped `newtron-nvp` and `kestrel-api`
drivers map them. Proven by the spec's validation and the interpreter's tests, the node's batch and
router tests, the gateway's tests against Postgres, and end to end (`run_lanes_test.go`: an input
push lands as the property and an `input-changed` event, a diagnostics line as a warning log line
on the component, an unclaimed push raw, none of them in the node's self-logs; a call lands as
`call-started` and `call-ended` with its id).
