---
title: "The task is derived read-only plumbing, projected from its interface"
date: 2026-07-14
status: "Accepted"
summary: "The `task` is **derived** read-only plumbing: creating an `interface` derives its one poll task, so task create/update/delete routes and the `task:create` / `:update` grants are dropped; `task.node_name` is removed and **projected** from `interface.node_name` (the worklist and telemetry owner-confinement join the interface), and a node purge cascades its interfaces and their tasks. Reverses the checkpoint-5d task-CRUD build; refines ADR-0039"
---

- **Date:** 2026-07-14 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [api](/architecture/api/)
- **Decision:** The `interface` is the **only authored** collection primitive; the `task` is **derived**.
  Creating an interface **derives its one poll task**, so the task surface is read-only (`GET /tasks`,
  `GET /tasks/{id}` only): the `POST` / `PATCH` / `DELETE /tasks` routes and the `task:create` / `task:update`
  grants are removed. A task carries **no node column**; `task.node_name` is dropped and its placement is
  **projected** from `interface.node_name`, so the worklist and the telemetry owner-confinement join the
  interface rather than reading a task-local node. A **node purge cascades** its interfaces and their derived
  tasks (`interface.node_name` and `task.interface_id` are `ON DELETE CASCADE`).
- **Context:** The checkpoint-5d build gave both primitives a full CRUD surface and a node placement of their
  own. That let an operator author a task divorced from its interface, and left a task's node and its interface's
  node as two independently-set fields that could disagree. The reframe makes the interface the one thing an
  operator authors (an API on a component, [ADR-0039](#adr-0039-an-interface-is-a-device-api-the-interface-type-is-its-transport-not-its-driver)):
  a reachability check is an interface, its poll task is the plumbing that runs it, and placement is a property
  of where the interface is reached from, stated once. This is the honest shape for the reach tier; the richer
  driver-authored collection surface (multiple functions over one interface) is a later slice and does not
  reintroduce operator task CRUD.
- **Refines:** [ADR-0039](#adr-0039-an-interface-is-a-device-api-the-interface-type-is-its-transport-not-its-driver)
  (the interface is the authored API; this ADR settles that its task is derived, not co-authored).
