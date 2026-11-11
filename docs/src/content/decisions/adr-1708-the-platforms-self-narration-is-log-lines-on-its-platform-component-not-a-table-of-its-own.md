---
title: "The platform's self-narration is log lines on its platform component, not a table of its own"
date: 2026-11-11
status: Accepted
summary: "What the platform tells about its own work (first, the server's housekeeping: rows pruned, hours closed into trends, days folded into blocks, partitions rolled and created, a failed pass) lands as `log_line` rows on the telling process's platform component, source `omniglass-server`, facility `retention`, never as events. The separate `internal_log` table #1094 planned is dropped"
refs: [1708, 1094, 589, 1629]
---

- **Date:** 2026-11-11 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/), [workers](/architecture/workers/), [properties](/architecture/properties/)
- **Decision:** (#1708.) The platform narrates itself in `log_line`, owned by its platform component. The server's retention pass writes one line per kind of work it did (info) and a warning when it fails, and the boot's partition check writes one when it creates partitions, through the Storage Gateway, beside the figures #1629 already lands on that component. A pass that did nothing writes nothing; no pass raises an event. A failed line write is a process-log warning, never retried and never failing the pass. The design-stage `internal_log` table is not built.
- **Consequences:** One read (the component's logs) carries what the server did, filterable by facility and severity and pruned by the same severity bands as every other line. Reconcile's planned per-run narration points at `log_line` on the server component too. The glossary's `internal_log` rows stay until #1126 moves the built glossary terms; this decision is their removal.
- **Context:** The storage page held a separate `internal_log` table for platform self-narration (startup, reconcile, migration, node-reg, config-sync), planned under #1094. The platform component (#1625, #1671) gives the server an owner that `log_line` (#589), the component-owned narration table, already serves: severity, facility, attributes, retention and the read all exist. Housekeeping told only stdout until now, which an operator of a hosted install cannot read.
- **Not chosen:**
  - **Build `internal_log`.** A second narration table with its own retention, its own read and its own write path, for lines whose owner already exists as a component.
  - **An event per pass.** A pass is routine work, not a condition; an event each pass would flood the event stream and bury the events an operator acts on. A failure is a warning line and the `retention-pass-failure-count` figure, which a rule can raise on.
  - **Receipt pruning narrated too.** It runs continuously at debug and is not narration an operator acts on, so it stays in the process's log.
