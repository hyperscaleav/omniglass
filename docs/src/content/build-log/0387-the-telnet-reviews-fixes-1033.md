---
title: "The telnet review's fixes (#1033)"
date: 2026-10-23
refs: [1033]
---

Each with a test that failed first. A telnet negotiation reply carries its own write deadline, and the
line reader and login dialog clear theirs after each write, so a component that sends `IAC DO
TIMING-MARK` after an idle spell is answered rather than silently dropped (and the stale deadline can no
longer surface from a read as the reader's own interrupt). The telnet probe reads a port that accepts
and hangs up as `closed`, not `timeout`, and while the node holds an up telnet session the probe takes
its verdict from the session instead of dialing a second connection. The design fences that still
listed telnet and ssh as unbuilt, and their endpoint-params rows, are retired.
