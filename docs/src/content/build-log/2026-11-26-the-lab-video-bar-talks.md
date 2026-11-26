---
title: "The lab's video bar calls, drifts, logs and sends its own events over two protocols (#2754)"
date: 2026-11-26
refs: [2754, 1360, 1145]
---

A simulator behavior can now report named events, as a route could (#1145): each renders against the
state its tick left, one whose data renders empty is not reported that tick, and a tick streams only
what it changed, so a timer rewriting a watched path with the value it holds adds nothing to the feed
(ADR-2754). A behavior also reads its device's wall clock (`now`, `nextSlot`) and draws integers and
chances from its device's source (`randInt`, `chance`), so a test drives a simulated day on a pinned
clock (ADR-2754.2). The Kestrel VRoom uses both to talk on its own: every video bar in a lab keeps a
meeting schedule, a tick a minute, booking about half its 15, 30 and 60 minute slots on the half hour
(a 15-minute one on the quarter), starting up to 2 minutes early or 5 late. Its calls report
`call-started`, `participant-joined`, `participant-left` and `call-ended`, each also an `event: log`
line in the codec's idiom, beside a status line every 15 minutes and the uplink's degraded and
recovered lines; it drifts its call stats in a call and its own load every 30 seconds, and no tick is
faster than 15 seconds. A bar runs about 0.6 calls, 3 events and 9 log lines an hour, where the first
cut's call every 90 seconds would have been about 40 events and 50 log lines a second across a
1,000-system lab. A dial on the bar's own API still starts a call at once, the schedule skipping a
meeting that falls due under it, and it ends at hang-up or after 30 minutes. The catalog gained the metrics a codec's call reports
(`packet-loss-ratio`, a loss ratio where 0 is healthy, `jitter`, `bitrate`, `frame-rate`,
`speaker-level`; the microphones' level reuses `audio-level`) and a component's own load (`cpu-utilization`, `memory-utilization`,
`active-channel-count`), the properties a bar reports of itself (`app-mode`, `ucc-signin`,
`touch-pair`, `camera-state`, `video-resolution`) and the identity beside its serial
(`hardware-revision`, `hostname`, `ip-address`), and the events `participant-joined` and
`participant-left`, all in the boot seed. `kestrel-api` gained a status poll, its identity facts on its
system poll, the participant listeners and a log listener on its feed; `polaris-dsp-snmp` reads the
DSP engine's load, temperature, fan and active channels and its identity over SNMP. In the corporate
lab the divisible conference rooms' video bars get their websocket control socket (`kestrel-ws`) as a
second endpoint, through the standard's `control` list, and a new `make test-lab` gate holds the
headquarters conference bar to 20 metrics, 8 properties, a caught event and a log line after the
warm-up, dialing a call on it first. The lab's identity check counts the new identity keys as identity, not a class signal.
