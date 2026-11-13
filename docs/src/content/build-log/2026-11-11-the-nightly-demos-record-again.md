---
title: "The nightly demos record again (#1622)"
date: 2026-11-11
refs: [1622, 1595, 1626, 1762]
---

Seven demos failed the nightly run on main, each because the product moved under a tape that still
told the old story. The delivery loss demo reads the node's own hop by its bare kinds. The command
queue health demo shows a command held through a stream outage settling **failed** when its 15 s
settle window closes, never delivered late, and reads the session's connected state as `up`. The set
up monitoring walkthrough waits on the ping's round trip, since ICMP Reachable became a property
(#1595) and left Vitals. Four tapes that ran `lab measure` on screen (push lane ledger, lab measure
fleet, lab measure series, scale lab pushes) now run it unseen behind a caption naming the command,
as the other measure demos already do: the ack-floor settle (#1762) lengthened a measurement past
the 180 s budget, past a visible wait, and past what a runner could render into a GIF.
