---
title: "Platform health closes the loop over a server-held sliding window"
date: 2026-11-10
status: Accepted
summary: "Accepted: with a simulator connected, the server brackets each node on a timer and reads the closed loop over a ring of its last ten brackets, a sliding window held in memory per process, so a loss reads while it is current and clears once the window passes it. Rejected: a since-start total, which carries every old loss into every read after it, and storing the window as platform metrics. The reconcile moved into a pure internal/closedloop package the lab and the server both use. Divergences: the window is ten brackets, the bracket is configurable from 1s to 1m, a scoped component read is a 404, the server assumes the nodes' cadence from its own settings, the connect gap pools every listener, received above 0 became reached, and impairment attribution is node-wide."
refs: [1647, 1639, 1644, 1662, 1249, 1634, 1720, 1721, 1722, 1723, 1724]
---

- **Date:** 2026-11-10 | **Status:** Accepted | **Pages:** [simulation](/architecture/simulation/), [api](/architecture/api/), [ui](/architecture/ui/), [views](/architecture/views/)
- **Decision:**
  1. **A sliding window, not a total since start.** The simulator's and the node's counters only grow
     from the moment each starts (#1639), so a read of their totals would carry an hour-old loss into
     every read after it, and a repaired device would never read healthy again. The server instead
     brackets each node with a connected simulator on a timer (the simulator, the node, the simulator
     again, as `lab measure` does) and keeps the last brackets in a ring; the window is the oldest
     against the one before the newest, and the newest counts the window's receipts, so a batch the bus
     acknowledged just before the window's end has the next bracket to land. A loss reads while it is
     inside the window and clears once the window slides past it.
  2. **The ring is in memory, per server process.** It is not stored as platform metrics: a server that
     restarts warms up again, and a second server holds a ring of its own. Storing the gaps is a later
     cut if a history of them earns its place.
  3. **One reconcile, in `internal/closedloop`.** The hop-by-hop reconcile moved from the lab harness
     into a pure package with its loss ratios, its ring and the node cadence it assumes, so the server
     never imports the lab and `lab measure` reads the same arithmetic.
  4. **The window is ten brackets of a configurable bracket.** The ticket's window was eleven brackets
     at a fixed minute. The ring keeps `RingSpan` (10) brackets of window plus the one after it and one
     more, and a node reads its first, one-bracket window after three brackets, growing to ten. The
     bracket is `OMNIGLASS_CLOSED_LOOP_BRACKET`, from 1s to 1m (the default), never past the receipt
     horizon: a lab you run brackets every 10s (a 100s window) and the throwaway labs the tests and
     walkthroughs run every second (a 10s window), so an impairment's attribution clears inside a
     walkthrough's budget. Receipts are counted from `ReceiptMargin` (30s, the stream's ack wait)
     before the window's start.
  5. **The server assumes the nodes' cadence.** Which poll targets were due over the window depends on
     how often the node checks in and probes, which the node does not report. The server reads it from
     `OMNIGLASS_CLOSED_LOOP_HEARTBEAT` and `OMNIGLASS_CLOSED_LOOP_PROBE_EVERY`, which a lab sets from its
     cadence profile; a deployed server whose nodes run another cadence must be told it (#1722 has each
     node report its own).
  6. **The gate is `simulation:read`, and a scoped component read is a 404.** The devices in the window
     are unplaced simulator state spanning every component a node simulates, so the read needs an
     all-scope `component:read`. Short of it the route answers 404, as the ticket specified, where the
     node's observation read answers 403; the page says the read needs a fleet-wide component read.
  7. **Connect-not-accepted pools every listener.** A connection is counted where a face accepts it, and
     the node tallies the dial, not the listener it reached, so the gap is one row per node, labelled
     `any listener`. A dial that never opens is not tallied, so kernel loss on tcp opens no connect gap
     (#1723). A websocket or wss probe tallies under `websocket`, where the simulator counts it.
  8. **"Received above 0" became "reached".** A tcp reachability probe, or an ssh or telnet login that
     asks nothing, draws a connection and no request, so its protocol's received count is honestly 0.
     The end-to-end check holds each polled protocol to received or accepted above 0, and the protocol
     table shows Accepted beside Received.
  9. **Impairment attribution is node-wide.** The node's tally carries no device, so a gap cannot be
     pinned to the device that lost it: every gap on a node whose simulator has any impairment applied
     reads `impaired` (#1662 narrows it). The device table carries each device's own impaired flag.
  10. **Two figures are not wired, and the backlog is 0.** The node does not report its dispatch queue
      depth or busy workers to the server (#1634, #1249), so they read "not wired yet", never 0. The
      kernel backlog masking term is read only by the lab, so the server's is 0.
- **Context:** #1639 built the closed loop for a scale lab's one-off measure. #1644 built Platform health.
  This puts the loop on the page, where an operator can break a simulated device on purpose and watch the
  platform account for the loss. Packet loss opens a gap only where the protocol rides on datagrams: snmp
  loses the request, while tcp retransmits, and a connect that never completes is not tallied as sent.
  The docs-shots lab runs no simulator, so the section has no screenshot yet (#1720); per-device answers
  (#1721) and the system a repeated device name belongs to (#1724) are later cuts.
