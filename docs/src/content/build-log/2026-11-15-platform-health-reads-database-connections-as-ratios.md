---
title: "Platform health reads database connections as loss ratios (#1980)"
date: 2026-11-15
refs: [1980, 1623]
---

The health page showed the server's pool as connections in use of its size plus a raw count of
acquires that waited, which cannot say whether 40 waits is a starved pool or a blip, and it did not
measure the database's side at all, so Postgres could run out of connections while every pool showed
headroom. The pool now carries its acquire count: the window's acquires is a new seeded metric
(`db-pool-acquire-count`), **Pool Waits** reads against it as a share, and **Pool Wait Time** is on the
page beside it. The server also reads `pg_stat_activity` through the Storage Gateway each window and
writes the connections held against the limit that admits clients (`max_connections` less
`superuser_reserved_connections`), those idle in a transaction against those held, and the age of the
oldest open transaction, as `db-connection-count`, `db-connection-limit`,
`db-connection-idle-in-transaction-count` and `db-transaction-oldest-age`. Held counts client and
walsender backends only, the ones `max_connections` admits, not the background workers the view also
lists. A read the database cannot answer writes nothing for that window. `TestPoolReadingsAreTheWindowsDeltas`
and `TestPlatformServerGroupsReadConnectionsAsRatios` failed before the change, and
`TestConnectionStatusReadsTheDatabasesOwnSide` proves the read against a real Postgres with a
connection left idle in a transaction. The scaling page's scale-up-or-out section and the operator and
collection pages teach each reading.
