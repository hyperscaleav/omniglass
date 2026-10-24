---
title: "The ssh review's fixes (#1032)"
date: 2026-10-23
refs: [1032]
---

Each with a test that failed first. The trusted host key publishes on every authenticated connection
and again on every tick the worklist delivers no pin, and the node carries it across a session's
recycle, so a lost publish followed by a credential edit cannot reopen first use to whatever answers
next. A keyboard-interactive question nothing declared answers (a one-time code) is an `auth`
refusal, not a retry loop. An ssh session requires `session.credential` (the ssh user), and the
`ssh-key` secret type ships so the private-key path works end to end, proven with the pty and exec
channels against a real ssh server. The `:retry` route says a `host_key` recycles any session onto the
new pin. The ssh probe's params login, which ignores the pin, is filed as #1060.
