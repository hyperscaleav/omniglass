---
title: "Actuation is a per-node pull, rendered at dispatch"
date: 2026-08-28
status: "Accepted"
summary: "The command wire is a per-node pull (`og.v1.command.<node>`, request-reply like the worklist) with the binding's request rendered server-side at dispatch: at-least-once delivery (redeliver after silence, a delivery TTL past which settlement's timed-out covers abandonment), execution idempotent per command id at the node, the report (`og.v1.commandstatus.<node>`) stamping `executed_at` once under placement confinement, and the report never the verdict: settlement stays the judgment of observed against intended"
---

- **Date:** 2026-08-28 | **Status:** Accepted | **Pages:** [commands](/architecture/commands/),
  [messaging](/architecture/messaging/), [collection](/architecture/collection/)
- **Decision:** The command wire (#815) is a **pull**: a node asks `og.v1.command.<node>`
  (request-reply mirroring the worklist, the same reply-inbox confinement and per-node subject
  grant) and receives the commands whose owning component has a driver-attached endpoint placed
  on it and whose driver binds the command's type. The binding's request template renders
  **server-side at dispatch** (`${value}` the intended value, `${arg.key}` an issue param,
  references validated at spec write), so the node executes a finished instruction and holds no
  template logic; an unrenderable binding records terminal `exec_error` instead of redelivering.
  Delivery is at-least-once: the pull stamps `dispatched_at`, silence redelivers after an
  interval, and a delivery TTL bounds the queue (a command no node picks up falls to settlement's
  `timed-out`). Execution is idempotent per command id at the node (a redelivery repeats the
  report, not the actuation), and the report on `og.v1.commandstatus.<node>` stamps
  `executed_at` once, under the same placement confinement the worklist carries. The report is
  **not** the verdict: any device answer counts as actuated, and whether the device did it stays
  settlement's judgment of observed (the driver's polls, #814) against intended.
- **Context:** The epic reserved the subject and demanded at-least-once delivery with idempotent
  execution and per-node isolation. A pull over the existing request-reply grammar reuses the
  proven worklist confinement and needs no JetStream API grants per node; a durable JetStream
  per-node consumer remains open as the scale evolution (#430's topology). Rendering at dispatch
  keeps secrets and template semantics server-side and makes the delivered payload inspectable;
  the execution arc (`dispatched_at`, `executed_at`, `exec_error`) lands beside the settlement
  arc rather than inside it because "the device was told" and "the device did" are different
  facts, and conflating them is how a wire ack gets mistaken for an outcome.
