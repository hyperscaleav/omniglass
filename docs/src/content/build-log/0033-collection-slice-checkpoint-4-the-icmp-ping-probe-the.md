---
title: "Collection slice (checkpoint 4): the icmp (ping) probe, the second capability primitive"
date: 2026-07-19
refs: []
form: bullet
---

- **Collection slice (checkpoint 4): the icmp (ping) probe, the second capability primitive.** `omniglass node`
  now also runs a real **icmp reachability probe** (`collection.NewICMPPinger`, unprivileged SOCK_DGRAM ICMP via
  `pro-bing`), emitting `icmp.reachable` (always present, `1`/`0` with a `reason` label) and `icmp.rtt_avg` (ms,
  absent when unreachable). It rides the checkpoint-3 pipeline unchanged: the same protobuf `Event`, JetStream
  stream, and owner-confining reject-not-project consumer carry it (the consumer does not branch on probe type).
  The capability question is the point of the primitive: a once-cached loopback self-check tells "this node
  cannot do ICMP at all" (an error, inconclusive, no datapoint) from "this target did not answer" (DATA:
  `icmp.reachable=0` with a down reason), and per the capability-wrapping doctrine a fake-`Pinger` unit test does
  not close the increment, a real-socket integration test (loopback echo + a TEST-NET-1 unreachable address where
  `received==0` is data, not an error) is the closing gate.
