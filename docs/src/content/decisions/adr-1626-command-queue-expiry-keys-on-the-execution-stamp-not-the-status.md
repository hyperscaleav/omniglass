---
title: "Command queue expiry keys on the execution stamp, not the status"
date: 2026-11-09
status: Accepted
summary: "Accepted: #1626 measures every node's command queue on its platform component. A command keeps its first delivery and its delivery count beside the last delivery. The server reads the whole fleet's queues on a 15 s timer through the pull's own candidate fragment, and counts a command as expired when it ages past the delivery TTL with no execution report, whatever its recorded status, split into undelivered and unexecuted. The wire writes one dispatch and one execution latency sample per command in the stamp's transaction. Two partial indexes serve the read. The loss ratios derive from the counts and the samples and take no key. An idle queue's pending age is written as zero, not left absent as the ticket had it, and the window's redelivery and expiry counts are written every window, zero when quiet."
refs: [1626, 1625, 1630, 1631, 1641, 1094]
---

- **Date:** 2026-11-09 | **Status:** Accepted | **Pages:** [commands](/architecture/commands/), [nodes](/architecture/nodes/), [collection](/architecture/collection/)
- **Decision:**
  1. **Expiry keys on `executed_at` and age, not on status.** `command-expired-count` counts the
     candidates that crossed `deliveryTTL` in the window with `executed_at` null, as instance
     `undelivered` (`first_dispatched_at` null) or `unexecuted` (taken, never reported). The recorded
     status says nothing about the wire: a fire-and-forget command is inserted `settled` today (and
     `issued` once #1630 lands), and a status-keyed count would miss it either way. The execution
     stamp is the one fact that says the node did the work, so the count reads the same before and
     after #1630.
  2. **A command keeps its delivery history.** The dispatch stamp keeps `dispatched_at` as the last
     take, sets `first_dispatched_at` on the first take only and counts every take in
     `delivery_count`. The API reads them as `first_delivered_at` and `delivery_count`.
  3. **One candidate resolution.** The pull and the health read share one SQL fragment
     (`commandCandidates`), so the depth the read reports cannot disagree with what a pull delivers.
     The health read widens its lower age bound to the window's start, so a command that expired in
     the window still resolves to its node, and groups the whole fleet in one statement by the node
     the chosen endpoint is placed on.
  4. **Two partial indexes, not one.** `command_unexecuted_idx` on `command (ts) where executed_at is
     null` serves the candidate scan, which the existing `status = 'issued'` indexes cannot (a
     fire-and-forget row is not `issued`). `command_redelivered_idx` on `command (dispatched_at)
     where delivery_count > 1` serves the redelivery count, because a command redelivered and then
     executed has left the candidates. If #1631's deliverable index lands, it replaces the first.
  5. **The window is the previous read's database time.** The timer holds the last good read's
     `now()` and passes it as the next window's start. The first read writes the gauges and no counts,
     since it cannot know what crossed before it. A failed read keeps the window open; a failed write
     after a good read moves it on, so a count is never written twice. The redelivery count reads
     the same window shifted 5 s back: a take is stamped at its transaction's start, so a read that
     runs before it commits cannot see it, and a window ending at the read's own time would have
     passed the stamp by the next read. The shifted windows still tile, so a take is counted once,
     up to 5 s late.
  6. **Latencies are written by the wire.** `command-dispatch-latency` lands in the dispatch's
     transaction on the first take only, and `command-execution-latency` in the execution report's,
     each a nested savepoint, so a refused platform write never loses the stamp. A command no node
     takes writes no dispatch sample and shows in `undelivered`.
  7. **Loss ratios derive, not stored.** Delivery loss is undelivered over (dispatch samples plus
     undelivered); execution loss is unexecuted over (execution samples plus unexecuted). Both are 0
     healthy and need no key, in line with #1641's uniform loss reading.
  8. **An idle queue's pending age is zero, not absent.** The ticket's table had `command-pending-age`
     absent when nothing waits. A gauge written only while something waits leaves a drained queue's
     last age standing as its latest sample beside a pending count of 0, and a rule raised on the
     age never sees the sample that clears it. The timer writes 0 instead, as the check-in age is
     written every tick: 0 is healthy.
  9. **The window counts are written every window, zero when quiet.** `command-redelivery-count`
     and `command-expired-count` (both instances) are written at every read that has a window, 0
     when nothing was redelivered or expired. Written only when nonzero, the latest value stood on
     the last event's count (a single redelivery read as 1 in the vitals long after it), the same
     frozen fact item 8 fixes for the pending age. A first read still writes neither, since it has
     no window.
- **Context:** A redelivery overwrote the only dispatch stamp, a failed pull went uncounted, and a
  fire-and-forget command no node took read `settled` forever, so the command lane had no baseline
  for #1249's separate node queues. The queue figures are platform metrics on the node's component
  (ADR-1625) so an operator reads them the way they read any component.
