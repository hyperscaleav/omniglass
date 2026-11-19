---
title: "The lab judges an owner by its pid and start time, so a reused pid reads as gone (#1258, #1400)"
date: 2026-11-14
refs: [1258, 1400, 1270]
---

The lab decided whether a previous lab's owner was still running by its pid alone. A lab killed
outright whose pid an unrelated process later held made every `omniglass lab run` refuse with a false
"A lab is already running here", and on a shared host the throwaway-lab cleanup could judge another
session's live lab by the wrong process. Every resource now also carries the owner's start time
(`omniglass.lab.pidstart`: `/proc/<pid>/stat` field 22 on Linux, the kernel process table on macOS,
`GetProcessTimes` on Windows), and the reaper compares host, PID namespace, pid and start time
together: a pid held by a process that started at another time reads as gone. A resource with no
start label (a lab started by an older binary) is never judged gone and never removed; the refusal
names `docker compose -p <project> down` as the way out. Unit tests cover the reused pid, the live
owner, the gone owner and the missing label for both the project and the throwaway paths, and the
real-daemon lab test now reaps a lab whose owner's pid is held by another process.
