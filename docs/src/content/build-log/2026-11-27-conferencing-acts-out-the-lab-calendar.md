---
title: "Conferencing acts out the lab calendar: the video bar, the codec and the meeting platform (#2767)"
date: 2026-11-27
refs: [2757, 2767]
---

A held meeting is now a call on the room's conferencing gear and on the meeting platform, all of it
read off the lab calendar and the world the simulator holds. The Kestrel VRoom's `call_cycle` and the
new Relay C1 `call_cycle` read `meetingNow`: one call per held meeting (none for a no-show, one for a
walk-in), started and ended at the first tick on or after the meeting does, participants joining and
leaving inside who came, content shared now and then, and a meeting that starts as the last ends
moving the call on in one tick. The #2773 `room_calendar` probe with `GET /api/room` retires, and the bar's own
plan and slot bookkeeping (#2754) stays as the fallback for a bar whose room has no calendar; a dial or a hang-up on the device holds against
the calendar. The UCC platform's `rooms_calendar` writes each room the world places from
`ownerMeetingNow` and posts signed `room.meeting.started` and `room.meeting.ended` webhooks at the
boundaries. The lab test for the world now holds a corporate lab's bars and platform to the plan's
own calendar through their real surfaces. No sim func was added.
