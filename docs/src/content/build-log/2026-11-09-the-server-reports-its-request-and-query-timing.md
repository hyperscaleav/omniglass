---
title: "The server reports its request and query timing on its own platform component (#1627)"
date: 2026-11-09
refs: [1627, 1094, 1625, 1628, 1342, 1672]
---

Each server boot ensures one `omniglass-server` component in the platform system for its identity,
`OMNIGLASS_SERVER_NAME` (the hostname when unset), keyed in a new `platform_server` table, and the
server lands its own figures there every report window. A middleware records each API request under
its method, route template and status class (`GET /api/v1/components/{name} 2xx`), with every
unrouted request under one `unmatched` instance, as `http-request-count`, `http-request-duration` and
`http-request-duration-max`. A pgx query tracer logs a query past the new `database.slow_query_ms`
setting as a WARN naming its statement's fingerprint, never its arguments, and counts it into
`db-slow-query-count`; the pool's connections, cap and exhausted-acquire waits land as the `db-pool-*`
keys. The window recorder and reporter in `internal/platformmetric` are the general primitive the
runtime figures (#1628) build on next. A fleet view that fails now logs its cause. The lab names its
server `server`; giving each chart replica a stable identity is #1672, and rendering the settings
reference is #1342. The decisions are
[ADR-1627](/architecture/decisions/#adr-1627-a-server-reports-its-request-and-query-timing-by-route-template-and-statement-fingerprint).
