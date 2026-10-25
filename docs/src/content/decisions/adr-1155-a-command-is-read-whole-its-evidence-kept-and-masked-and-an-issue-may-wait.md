---
title: "A command is read whole, its evidence kept and masked, and an issue may wait"
date: 2026-10-25
status: Accepted
summary: "A command reads back from issue to settlement (#1155): GET /components/{name}/commands and /commands/{id}, gated command:read on the component tier with the read and action split, carrying each transition's time, the in-flight phase (derived, never stored), the settle mode, the evidence and a reason. The row keeps the reply, masked of the dispatched endpoint's secrets and of credential-labeled pairs before it is stored, and what decided the outcome; each terminal status lands command-settled, command-failed or command-timed-out, joining command-lifecycle. An issue may wait (1 to 120 seconds, 64 waiters, 4 a principal, a 429 before anything is issued), judging the command at its deadline"
refs: [1155]
---

- **Date:** 2026-10-25 | **Status:** Accepted | **Pages:** [commands](/architecture/commands/),
  [api](/architecture/api/), [identity and access](/architecture/identity-access/),
  [metrics and properties](/architecture/properties/), [glossary](/architecture/glossary/)
- **Decision:** (#1155, approved in session on 2026-10-25.)
  1. **A command is read whole, under its component.** `GET /components/{name}/commands/{id}` and
     the history, `GET /components/{name}/commands` (newest first, narrowed by status, by when it was
     issued, and a page size), answer the command type and its intended value, the target, the
     recorded status with a time per transition (issued, delivered, executed, settled), the endpoint,
     node and driver that carry it, the settle mode its binding declares, the evidence, a reason,
     and the issuer. A command has no name, so it is addressed by its id under the component, which
     takes every spelling the component routes take; an id under another component is the same 404
     an unknown one is.
  2. **Reading needs `command:read`, resolved on the component tier with the split.** The component
     resolves through the caller's `component:read` (outside it, the non-disclosing 404) and its
     `command:read` (inside the read but outside that, the truthful 403), as an action's target does
     ([ADR-0116](#adr-0116-a-refusal-picks-its-status-from-the-difference-between-two-scopes)).
     The viewer floor holds `command:read`, and the read floor gives every issuer it on the grants
     that carry `command:issue`, so the issue's answer carries the read body too.
  3. **Where a command sits is derived, never stored.** The phase (queued, delivered, awaiting the
     reply, awaiting the observation, then the terminal status) and the settle mode (reply,
     observation, none) are functions of the row and its binding, computed on read, so no write path
     has a second fact to keep in step.
  4. **The row keeps its evidence and its why.** The execution report's reply and error are stored,
     masked first of every form of the dispatched endpoint's secret fields (and a basic login's
     authorization value) and of any pair the text labels a credential, then made valid text and
     capped at 4 KiB; the judgment still reads the reply whole. The outcome records `settled_by`
     (issue, reply, observation, window), a `reason` in operator words, and the observed sample when
     an observation decided it. A timeout's reason says where the command stopped: answered with
     nothing observed, delivered with no report, queued for a node that never took it, or carried by
     no endpoint. The masking is one primitive, `internal/redact`, that the node's http scrub now
     calls as well.
  5. **Each terminal status lands its lifecycle event.** `command-settled`, `command-failed` and
     `command-timed-out` join `command-issued` in `command-lifecycle`, written with the outcome in
     its transaction and linked from the row (`settled_event_id`); a fire-and-forget command, settled
     by its issue, writes none. This amends
     [ADR-1100](#adr-1100-legacy-rows-and-stored-declarations-honor-the-one-writer-ruling) point 7,
     which named `command-issued` alone.
  6. **An issue may wait, bounded.** `wait` blocks the issue until the command is terminal or
     `wait_timeout` passes (1 to 120 seconds, 30 by default), polling from a tenth of a second back
     to once a second and judging the command itself once its deadline passes, so a wait answers a
     timeout at the window rather than at the next sweep. At most 64 waits run at once, 4 per
     principal; a wait past either is a 429 refused before anything is issued, so a refusal leaves
     nothing to retry around. A wait that runs out answers with the command as it stands.
- **Context:** The issue answered `{id, status: "issued"}` and nothing read a command back, so the
  #1154 demo could not show a `set-volume` settling and only an end-to-end test asserted it. The
  state existed on the row but the evidence did not: a failed command could say that it failed and
  not what the component answered, nor where a timed-out one had stopped. The reply crosses from a
  component the operator does not control, so it is masked before it is ever stored rather than at
  each read, and the wait holds a request open, so it is bounded in time and in count before it
  writes anything.
