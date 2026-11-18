---
title: "The lab's omnibot triages an alarm"
date: 2026-11-18
refs: [2163]
---

A recording for the server's omnibot can now hold conversations beside its default turns, each
chosen when the run's message contains its match, and a run counts its turns from that message, so
a second prompt in one conversation starts its own script rather than running off the end of the
first. The lab's recording gains a triage conversation, matched by the triage-alarm skill's heading:
it reads the meeting 405 video bar, its alarms and its reachability, and ends on an assessment with
no write. Every other prompt still plays the tag script. This is what lets a one-click troubleshoot
button (#2164) show omnibot answering in the lab.
