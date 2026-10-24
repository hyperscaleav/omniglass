---
title: "Collection slice (checkpoint 5d-api): the interface and task authoring backend"
date: 2026-07-19
refs: []
form: bullet
---

- **Collection slice (checkpoint 5d-api): the interface and task authoring backend.** The operator CRUD over the
  two collection primitives that the reachability read and the node worklist consume. The Storage Gateway gains
  scoped `Create/List/Get/Update/Delete` for both `interface` (name, type, owning component, node placement,
  params) and `task` (a content-addressed id over interface + mode + spec, so identical work dedupes), auditing
  every mutation in-transaction like the estate tiers; the Huma surface adds AIP CRUD at `/interfaces`,
  `/interfaces/{name}`, `/tasks`, and `/tasks/{id}`, regenerated into the OpenAPI document, the cobra CLI
  (`omniglass interface …` / `omniglass task …`), and the typed SPA client. Both authorization layers apply on
  every route: an `interface:<action>` / `task:<action>` **permission** (admin gains `interface:*` / `task:*`,
  operator keeps `create,update`, the `*:read` floor gives everyone read), and **scope** injected by the gateway.
  Neither primitive is a scope-tree entity of its own, so scope **cascades through the owning component**: an
  interface inherits its component's read/action scope, a task inherits its interface's component's, reusing the
  component tier with no new `scope_kind` (an operator scoped to a component subtree administers exactly that
  subtree's interfaces and tasks). A component-less (server-hosted) interface is reachable only under an all
  scope. An interface or task whose component is outside the caller's scope is a **non-disclosing 404** on read
  and update, and a create under an out-of-scope component is a 403, negatively tested at both the gateway and
  the API. No web surface yet: the flat Interfaces / Tasks pages and the "add check" affordance are checkpoint
  5d-ui.
