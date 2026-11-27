---
title: "A scenario scripts recurring problems on the lab calendar, applied by one executor (#2769)"
date: 2026-11-27
refs: [2757, 2769]
---

A scenario's `calendar.problems` scripts what goes wrong on a schedule: a cadence (daily, weekly on a
weekday, monthly on a date or the nth weekday), a UTC time and duration, a scope (a standard, a site or
its kind, or named rooms) with the slot it lands on, and one impairment (unplugged, packet loss, or a
pinned state path). The calendar derives them like the meetings, so every node computes the same month;
only an optional per-room `spread` is drawn, from the seed. `problem` is the calendar's second entry
kind, read through `Problems`, `ProblemsNow` and `All` and the `problemsNow` template function; `Entries`
is still the meetings alone.

The simulator's one problem executor opens each window and closes it: pins through the behavior-override
seam (the path is put back as it was), unplugged and loss through the netns impairer with the server's own
impairments merged in, refused with a log line without NET_ADMIN. The meeting platform lists a device
`offline` and a room with a dark codec or bar `offline` while a problem has it unplugged, read off the
calendar. The corporate scenario scripts three: boardroom displays dark on Tuesday afternoons, a weekly
dead wireless-mic battery and a nightly codec firmware window.
