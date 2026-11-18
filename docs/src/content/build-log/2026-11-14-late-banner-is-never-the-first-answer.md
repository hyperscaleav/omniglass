---
title: "A late banner is never the first answer (#1418)"
date: 2026-11-14
refs: [1418]
---

A held line connection's first ask used to draw its boundary with a 2ms drain window, so a banner a slow device sent after that landed armed and was taken as the ask's answer. The first ask now waits out a connect quiet window instead: 250ms from when the connection opened by default, never more than half the ask's own timeout, any line in it discarded as a banner. Later asks keep the 2ms window and pay nothing. A driver tunes it with `session.connect_quiet` (up to 10s, `0s` for a device that sends no banner); it rides every held line transport. A new real-socket test dials 50 connections to a server whose banner is delayed 20ms and asserts every first exchange is the answer, and a timing assertion pins that a second ask is not slowed.
