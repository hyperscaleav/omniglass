---
title: "The server's retention pass reports on its own platform component (#1671)"
date: 2026-11-09
refs: [1671, 1627, 1629]
---

The boot now ensures the server's platform component before the bus starts and hands its id to the
retention pass, so every pass, the boot's first included, lands its figures on the server's own
component in production, not only in a test that hands the bus an id. A failed ensure no longer
refuses the boot: it logs a WARN with the identity and the cause, and the server serves and prunes
with no platform figures (ADR-1671). `runServer` also returns when its context ends, as on SIGINT,
which lets the integration tests boot the real server path against a testcontainers Postgres: one
reads `retention-pass-duration` on the component `platform_server` names, the other boots an
identity of only spaces and watches the pass prune a planted log line while no platform server and
no retention figure land. The storage page drops its "until the server registers" caveat.
