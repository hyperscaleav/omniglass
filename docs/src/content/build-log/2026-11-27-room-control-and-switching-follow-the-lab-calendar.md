---
title: "Room control and switching follow the lab calendar (#2776)"
date: 2026-11-27
refs: [2757, 2776]
---

The conference standard now staffs a control system, a video switcher and a display endpoint (Newtron Core CP-3,
Meridian MX 84 and XL-1), beside the touch panel it already had. The processor holds the room's system power and
the last macro it ran (start-meeting as the room goes on, end-meeting as it goes to standby), the touch panel
wakes and changes page (idle, in-call, sharing), the switcher routes the conferencing input or the laptop while
content is shared and parks on the empty input between meetings, and the extender carries a signal while a meeting
is held. Two new template functions, `roomInUse` and `meetingSharing`, are the readings these share. A device in a
room with no calendar keeps its previous state. The bar's content sharing remains its own draw; moving it onto
`meetingSharing` is a follow-up.
