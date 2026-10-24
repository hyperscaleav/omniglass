---
title: "A telnet session with option negotiation and a login dialog (#1033)"
date: 2026-10-23
refs: [1033]
---

The second slice of the protocol backlog. `telnet` joins the transport registry as a held line
transport (`internal/transport`), its reachability probe connecting and waiting for the first
negotiation or banner bytes (`telnet-response-time`, seeded; a silent port reads reached but not
responsive). The held session is the line protocol's own behind a filter (`internal/collection/telnet.go`):
inbound IAC sequences are stripped (including one split across reads, and subnegotiations), DO is
answered WONT and WILL is answered DONT, and a literal 0xFF is escaped. The login is the shared dialog
from #1049, run as the session's auth step on establish and every recover. The simulated panel gains an
`nvp-telnet` face (port 23, a login whose prompts end no line, the NVP verbs and pushes behind it), and
the official `newtron-nvp-telnet` driver declares the login and the panel's terminal refusal. A poll
that faulted on its session's refusal now runs at the first tick its session is up again, so a fixed
credential lands data within a tick rather than an interval later. Proven against real sockets (the
filter and the probe), against the simulator (`telnet_session_hold_test.go`: login, poll, command,
push, a recover that logs in again and re-arms, and a wrong password connected exactly once and
rejected with the panel's own text), and end to end (`run_telnet_session_test.go`: the shipped driver
attached with a wrong password reads `rejected`, `session-reason` `auth: auth rejected` and
`endpoint-authenticated` `no`, logs one rejection across twenty ticks, a `:retry` dials once more, and
fixing the secret reads `up`, authenticated, and lands the polled value).
