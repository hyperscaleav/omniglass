---
title: "A server whose platform component cannot be ensured boots and reports nothing"
date: 2026-11-09
status: Accepted
summary: "Accepted: the boot ensures the server's platform component before the bus starts and hands its id to the retention pass, so the boot's own pass reports. A failed ensure, fatal under #1627, is now a WARN: the server serves and prunes with an empty id, and the retention pass and the self reporter write nothing."
refs: [1671, 1627, 1629]
---

- **Date:** 2026-11-09 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/), [collection](/architecture/collection/)
- **Decision:**
  1. **One component, ensured before the bus.** `runServer` ensures the identity's component and
     passes its id into `bus.Config.PlatformComponentID`, the seam ADR-1629 left empty, before the
     bus starts, so the pass the bus runs at boot lands its figures too.
  2. **A failed ensure is a warning.** Under #1627 a failed ensure refused the boot. It now logs a
     WARN naming the identity and the cause, and the boot goes on with an empty id: the retention
     pass prunes and writes no figure, and the self reporter does not start.
- **Context:** the retention figures were built behind a seam (#1629) until the server had a
  component (#1627). Self-monitoring is an account of the server's work, not a precondition of it.
- **Rejected:** keeping the refused boot (a bad `OMNIGLASS_SERVER_NAME` or a transient write
  failure would stop ingest and pruning to protect a report about them); retrying the ensure in the
  background (a next boot retries it, and the WARN names what to fix).
