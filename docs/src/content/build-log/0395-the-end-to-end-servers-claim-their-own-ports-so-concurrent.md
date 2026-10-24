---
title: "The end-to-end servers claim their own ports, so concurrent runs never collide (#1077)"
date: 2026-10-23
refs: [1077]
---

Every end-to-end test started `omniglass server` on ports it found with a pick-and-release helper:
bind `127.0.0.1:0`, read the port, close the listener, and hand the number to a binary that bound it
seconds later, after migrating and seeding. In that gap any other process on the host could be handed
the same port, so a run beside another (two worktrees, a session beside `make test`) failed with
`bind: address already in use` on the API, or `nats-server not ready for connections` when the bus's
port went. Now the server binds port 0 itself and reports what it got. `OMNIGLASS_ADDR` and
`OMNIGLASS_NATS_ADDR` may name port 0; the API binds before it serves, so its address is known; the
bus reads port 0 as ephemeral, where nats-server alone reads it as its default 4222; and a new
`OMNIGLASS_READY_FILE` names a path the server writes, whole or not at all, once both listeners are
bound: a JSON object of the bound `addr` and `nats_addr` and the `nats_url` it advertises, removed at
a graceful shutdown. The advertised bus URL, when `OMNIGLASS_NATS_URL` is unset, is now the address the bus bound
(loopback for a wildcard host) rather than a fixed `nats://127.0.0.1:4222`, which is the same URL for
every configuration that worked before and the right one for a bus on port 0. The end-to-end package
starts every server through one helper, `startServer`, which sets the listen addresses to port 0 and
reads the ready file, and a guard test fails any test that runs the server another way or sets its
listen addresses (against main's tests it names all seven server starts). Proven by the bus binding a
kernel-picked port for 0 (it bound 4222 first), the ready file's write, replace and removal, and the
healthz end-to-end test asserting both reported ports are live and neither is 0 or 4222 (it failed
first, no ready file). Beside a squatter that holds and churns ephemeral ports, standing in for a busy
host, main's healthz test failed four runs in four (one on the API's port, three on the bus's) and the
fixed one passed four in four; the end-to-end package run twice side by side, three pairs, passed every
run. The
[CLI guide](/guides/cli/#a-server-on-ports-it-picks) teaches the ready file, and the deployment guides'
configuration tables render the new variable from the registry.

The review's fixes, each with a test that failed first: the harness observes the server's exit by
closing a channel rather than sending its one value, since a server that died before it was ready
had its exit taken by the wait, leaving the cleanup that stops it blocked until the suite's timeout
(every later test aborted, the boot error never printed); the boot clears a ready file already at
the path before anything that can fail, since a server killed outright runs no shutdown and its file
named a dead run's ports, which the next boot left in place throughout, or for good when that boot
failed; and the test-driven page scopes the one-helper rule to `internal/e2e`, noting that the browser
tier and the walkthrough stack isolate through a private Docker network per run.
