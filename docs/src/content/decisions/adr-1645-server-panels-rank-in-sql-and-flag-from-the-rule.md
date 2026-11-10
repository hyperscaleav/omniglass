---
title: "Server panels rank in SQL and flag a panel from its alarm's rule"
date: 2026-11-10
status: Accepted
summary: "Accepted: #1645 renders each server's Requests, Ingest, Queues and Retention panels from storage.PlatformServerGroups. Both request keys are ranked, the busiest routes beside the slowest. A panel is flagged by an open alarm whose rule samples one of its keys, matched on the rule's sample_key. Server figures carry no window aging. A ranked key with no window reads as one not-reported row. A key's tooltip leads in-app onto its series. Walkthroughs gained hover and an off-camera ready wait, and docs shots gained a wait step."
refs: [1645, 1644, 1627, 1628, 1629, 1646]
---

- **Date:** 2026-11-10 | **Status:** Accepted | **Pages:** [ui](/architecture/ui/), [api](/architecture/api/), [Platform health](/guides/operator/platform-health/)
- **Decision:**
  1. **Both request keys are ranked.** The ticket ranked `http-request-duration-max` alone and
     returned `http-request-count` per instance. A route table is as large for one key as the
     other, so both are cut in SQL to the ten highest instances of the key's newest time, highest
     first: the slowest routes and the busiest. `PlatformServerKey.Rank` carries the cut, so a
     third ranked key is one field in `storage.PlatformServerGroups`.
  2. **A panel is flagged from its alarm's rule.** An alarm carries no key of its own. The view
     reads each server's open alarms with the `sample_key` of the rule that raised them, and the
     page flags the panel that holds that key. A hand-raised alarm, or one whose rule is gone, has
     no key and flags no panel; it still counts in the server's "open" badge. The page holds no
     threshold.
  3. **Server figures carry no window aging.** ADR-1644 shows a node's window count only while its
     window is the newest. The server's counts need no such filter: the request and slow-query
     counts write one zero after a window that counted something, and the pipeline and retention
     figures are written every window or pass. A server figure is its latest sample, per
     instance, unsummed.
  4. **A ranked key with no window is one row.** Before the first report window a ranked key has no
     instance to rank. It reads as a single not-reported row named by the key, not as a heading
     over an "owner-level" row.
  5. **Pairs and sizes.** A key read against its bound (`db-pool-acquired` of `db-pool-max`,
     `metric-oldest-day-age` of `retention-window-days`) prints as `a / b`. Byte figures print in
     KB, MB or GB. Routes wrap at their slashes.
  6. **Sparklines read only for an open set.** A closed server panel set asks for no series.
  7. **The tooltip leads onto the series.** A key's tooltip is its catalog description plus an
     in-app link to the server's platform component. `InfoTip` gained a same-tab link through the
     console router, so the link stays under the console's `/web` base.
  8. **The console e2e pushes a sample.** The lab's empty scenario has no collecting node, so
     nothing would land in an ingest window. The e2e pushes one telemetry sample, then reads each
     panel's figure after a report window.
  9. **Walkthroughs and docs shots learned to wait.** A walkthrough step can now `hover`, and a
     script can declare `ready`: a page and a target it waits for off camera before recording. The
     Requests panel fills only after the server's first report window, so this keeps the
     recording inside its budget. A docs shot step can `wait` for a selector, with a timeout. The
     panel grid's figures move with every capture, so the gate's baseline masks the grid whole.
  10. **The walkthrough follows a server figure.** It no longer opens a node's row. It opens the
      Requests panel, rests on the slowest route's key, and follows the tooltip's link to the
      series on the server's platform component.
- **Context:** #1644 shipped the page with its node table. #1627, #1628 and #1629 put the server's
  request, pipeline and retention figures on its platform component, and this slice reads them.
  Loss stays #1646's ratio. `bus-telemetry-drop-count` prints as its producer's count until #1641
  folds it into the uniform keys.
