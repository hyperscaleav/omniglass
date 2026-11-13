---
title: "The delivery read settles on the telemetry consumer's ack floor, not an empty consumer (#1758)"
date: 2026-11-11
refs: [1758, 1641, 1628, 1623]
---

The delivery read waited for the server's `bus-consumer-pending` and `bus-consumer-ack-pending` gauges
to read 0 together before it reconciled the window. Under steady load the consumer always holds the
batch in flight, so that moment never came: the read ran out its bound and named every boot
unsettled, and `make perf-check SIZE=laptop` failed `delivery_unsettled_boots` at 19 against a baseline
of 0.

What the read needs is narrower: every batch published by the time the window's last batches were
known published has been answered. The server now gauges two more figures each report window,
`bus-stream-last-seq` (the stream's head, read first) and `bus-consumer-ack-floor` (the durable's
ack floor, read after), and the read keeps every report of them since the window closed. The publish
moment is the latest mint of the batches that proved each boot settled, of the next boot's first batch
for a boot that ended, and the window's close. A report counts as reading the head after that moment
only when the report before it flushed at or after it, since a report stamps its time after it reads.
The read settles once any report from that one on carries an ack floor at or past that head; batches
arriving behind it no longer hold the read. A Term at the delivery bound advances the floor as an ack
does, so a dropped batch does not stall it.

The rule is a pure function over the report series, tested with a consumer that always holds batches
(it settles and reads every hop), a floor that never passes (it stays unsettled and its note names the
floor and the head), and a report stamped after the publish moment whose read came before it (not
counted). The unsettled note now reads `the ack floor reached 41, not yet past the stream's last
sequence 45`, or names the gauge never read. The both-zero rule is gone with no fallback.

At the platform's 60 s report window the settle can take four windows (the proving batch, a report
after it, the head read, the floor past it), so `--settle-timeout` on `lab measure` and `lab perf`
now defaults to 5 minutes, up from 3.
