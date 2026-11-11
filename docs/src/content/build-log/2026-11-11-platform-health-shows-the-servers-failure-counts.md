---
title: "Platform health shows the server's failure counts (#1769)"
date: 2026-11-11
refs: [1769]
---

The server's failure counts are on its panels: bus request failures against the requests on each path
and sweep failures against each sweep's passes, both `a / b`, telemetry ack failures on Queues, and the
retention pass's failure on Retention.

Each failure count is now written 0 beside a window of attempts that all succeeded, by one bus
primitive the push lane's refusal count already used. Before, a failure count landed only when it was
above 0, so a healthy path read "not reported" rather than `0 / n`, and a missing figure could not tell
a quiet path from a clean one.
