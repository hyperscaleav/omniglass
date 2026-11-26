---
title: "The lab's video bar calls, drifts, logs and sends its own events over two protocols (#2754)"
date: 2026-11-26
refs: [2754, 1360, 1145]
---

A simulator behavior can now report named events, as a route could (#1145): each renders against the
state its tick left, one whose data renders empty is not reported that tick, and a tick streams only
what it changed, so a timer rewriting a watched path with the value it holds adds nothing to the feed.
The Kestrel VRoom uses it to talk on its own. Every video bar in a lab places a call about every 90
seconds (`call-started`, `participant-joined`, `participant-left`, `call-ended`, each also an
`event: log` line in the codec's idiom), and drifts its call stats and its own load every 30 seconds;
no tick is faster than 15 seconds. The catalog gained the metrics a codec's call reports
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
warm-up. The lab's identity check counts the new identity keys as identity, not a class signal.
