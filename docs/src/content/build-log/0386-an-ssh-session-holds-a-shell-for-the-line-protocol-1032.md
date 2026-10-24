---
title: "An ssh session holds a shell for the line protocol (#1032)"
date: 2026-10-23
refs: [1032]
---

The third slice of the protocol backlog. `ssh` becomes a held line transport
(`internal/collection/sshsession.go`): a staged dialer whose auth step is ssh userauth with the
credential the driver names (`session.credential`: a password, a private key, or keyboard-interactive
answered by the shared login dialog), then a shell channel (a PTY on `session.pty`) or an exec channel
(`session.exec`) bridged onto an in-memory pipe so the line reader keeps its read deadlines. A refused
credential is terminal `auth`; the host key is **pinned on first use**: the node trusts the first key,
publishes it as the seeded `ssh-host-key` property, and the worklist delivers it back as the pin (the
declared value first), so a changed key reads `rejected`, class `host-key`, naming both fingerprints.
`POST /endpoints/{id}:retry` takes an optional `host_key`, which declares the new pin (audited) and
recycles the session onto it. The simulated panel keeps its ssh host key across listener restarts (a
test rotates it to model a replaced component) and serves the NVP subject on its ssh shell; the
official `newtron-nvp-ssh` driver ships. Proven against the simulator (poll, command, push, recover onto
the same key, down after the budget, a wrong password and a rotated key each rejected after one
attempt) and end to end (`run_ssh_session_test.go`: trust on first use lands `ssh-host-key` and the
worklist pin, a rotated key reads `rejected` naming both keys, a malformed `host_key` refuses, and the
accepted key reads `up`). Recorded as
[ADR-0157](/architecture/decisions/#adr-0157-telnet-and-ssh-are-held-line-transports-their-login-is-the-drivers-and-the-ssh-host-key-is-pinned-on-first-use),
which also logs the designed endpoint-params login retiring. Prompt-delimited answers are #1057.
