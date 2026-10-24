---
title: "Collection slice (checkpoint 5g): the derived interface/task model"
date: 2026-07-19
refs: []
form: bullet
---

- **Collection slice (checkpoint 5g): the derived interface/task model.** The reachability primitives were
  reframed to their honest shape ([ADR-0040](/architecture/decisions/#adr-0040-the-task-is-derived-read-only-plumbing-projected-from-its-interface)):
  the **interface** is the only authored primitive and the **task** is **derived**. An interface is now
  **named by its protocol** (the name derives from its `interface_type`, unique per component), so the create
  surface takes a type, not a free-text name. Creating an interface **derives its one poll task**, so the task
  surface is **read-only**: the `POST` / `PATCH` / `DELETE /tasks` routes and the `task:create` / `task:update`
  grants are gone, leaving `GET /tasks` and `GET /tasks/{id}`. `task.node_name` is dropped and **projected** from
  `interface.node_name` (the worklist and the telemetry owner-confinement now join the interface for placement),
  and a **node purge cascades** its interfaces and their tasks (`interface.node_name` and `task.interface_id` are
  `ON DELETE CASCADE`). The console follows the entity model, folding the collection children onto their parents:
  the standalone Tasks page is removed and a node's derived tasks read as a **panel on the node detail**, and the
  standalone Interfaces tab is removed and a component's interfaces (with their reachability, add, and edit/delete)
  read as a **panel on the component detail** (an interface belongs to its component). The interface create form
  drops the name input (name derives from the protocol), the node detail moves onto the shared read-edit-save blade,
  and the collection pages route through the shared Button primitive.
  Storage, the API, the CLI, and the typed client regenerate from the reduced surface, so `make gen` is clean.
  Proven by the reduced storage, API, and web suites over the derived model (an interface derives its task, the
  task surface is read-only, placement projects, a node purge cascades). This keeps the [collection](/architecture/collection/)
  and [nodes](/architecture/nodes/) pages `Partial`: the driver / collect layer (the normalized menu, SNMP, the
  `$sec:` / `$var:` interpolation consumer, templates) is still Design.
