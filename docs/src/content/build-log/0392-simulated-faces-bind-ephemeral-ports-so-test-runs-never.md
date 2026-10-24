---
title: "Simulated faces bind ephemeral ports, so test runs never collide (#1066)"
date: 2026-10-23
refs: [1066]
---

Every simulator-backed test bound each face of its simulated component at a fixed loopback address
and port (127.89.16.5:20023 and its like, after the privileged-port lift), so two `go test` runs on
one host (two worktrees, or a session beside a `make test`) failed with `bind: address already in
use`; #1061's tests had taken a fresh address per stack as a stopgap, which isolated a `-count` rerun
but not a second process. Now a face binds whatever port the engine's port map asks for and records
the address it got: `Device.Faces` lists each face with its surface's name, protocol, declared port
and bound address. The map is the explicit choice: fixed (no map, or `PrivilegedPortShift`) for the
operator-facing simulator, whose ports an operator, a seeded endpoint and the docs all name, and
`sim.EphemeralPorts` for tests, where every face binds port 0 and the kernel hands it a free port. A
face reports the port it bound everywhere it reports one (its hits, its session events, SNMP's request
detail), where a face asked for port 0 had reported 0; SNMP binds before it builds its
agent so it knows the port, and a simulated component's raw-TCP faces find their session registry by
the declared port rather than the mapped one. Every in-process simulator test (the sim package, the node's session
tests) serves on ephemeral ports and aims its clients and endpoints at the reported addresses. The
end-to-end and run-mode tests drive `omniglass sim run`, which binds the profile's ports (lifted) on
the address an endpoint targets and cannot tell the endpoints a port it picked, so they stay on fixed
ports and claim a loopback address no other test holds (`simtest.ClaimLoopback`, a sentinel listener
held for the test's life); so do the engine tests that pin a port on purpose (an address handed over
or swapped, a squatted port released and rebound). Proven by a test that serves one address from two engines at once
and finds every face of both bound, answering on its own engine at its reported address, with the
bound port on its hits and session events (it failed first, the line protocol's session events
reporting port 0), and by the sim, node and end-to-end packages run twice side by side with no
collision, where on main the same pair failed with bind errors. The
[simulation](/architecture/simulation/#fixed-ports-and-ephemeral-ports) page teaches the two choices.
