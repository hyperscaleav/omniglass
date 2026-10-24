---
title: "System roles, required capabilities, and the assignment guard"
date: 2026-07-21
refs: []
form: bullet
---

- **System roles, required capabilities, and the assignment guard.** A system now declares **what it needs
  filled**. **`system_role`** is the slot (a table microphone, a main display), declared either on a
  **`standard`**, where every conforming system **inherits it live**, or **directly on one `system`** (ad-hoc,
  which is how a one-off system gets roles at all). Both owners ride the **same exclusive arc `property_value`
  uses** (`owner_kind` plus `standard_id` / `system_id`, a one-set CHECK, and a `unique nulls not distinct` key
  over the arc columns and the role name, since the arc leaves one owner column NULL). A role carries a
  **`quorum`** (how many components should fill it, floored at one) and requires a **conjunctive** set of
  **`role_capability`** rows: a component must provide **every** listed capability. Two more tables complete it:
  **`component_capability`** (`component_id`, `capability_id`, `present`) is the component's **own** capability
  facts layered over its product's (`present=true` adds one the product does not claim, `present=false`
  suppresses one it does), and **`role_assignment`** records who fills the role in this system, with the
  component FK **`on delete restrict`** so a component staffing a role cannot be deleted out from under it. Two
  resolvers ship with them. **`EffectiveCapabilities(component)`** is the product's capabilities UNION the
  component's additions MINUS its suppressions, so a **productless component resolves to just its own
  declarations**; it is the single definition of "what this component can do" for the whole platform.
  **`EffectiveRoles(system)`** merges the inherited arm (`from_standard` true) with the ad-hoc arm, each role
  carrying its required capabilities, its quorum, its assignments, and the served **`assigned`** and
  **`understaffed`** counts, so no surface does the arithmetic itself. The slice's decision is the
  **guard**: **`AssignRole` refuses (422) when the component's resolved capabilities do not cover the role's
  requirement, and the refusal NAMES the missing ones** (`component "panel-1" cannot fill role "table-mic":
  missing microphone, speaker`, sorted so the same gap always reads the same way), a refusal on **modeled**
  grounds in the same class as the location placement constraint, and named for the same reason. Capabilities became a **resolved** set precisely to make that
  strictness survivable: `product` is optional on a component, so a guard over a product-only fact would have
  locked every productless component out of every role. Eight routes ship, regenerated into the OpenAPI
  document, the cobra CLI, and the typed SPA client: `GET /standards/{id}/roles` plus
  `PUT` / `DELETE /standards/{id}/roles/{role}` (gated `standard:read` / `:update` / `:delete`);
  `GET /systems/{name}/roles` (the resolved read) plus `PUT` / `DELETE /systems/{name}/roles/{role}` and
  `PUT` / `DELETE /systems/{name}/roles/{role}/assignments/{component}` (gated `system:read` / `:update`); and
  `GET /components/{name}/capabilities` plus `PUT` / `DELETE /components/{name}/capabilities/{capability}`
  (gated `component:read` / `:update`). Every system and component route resolves its owner **within the
  caller's scope** first, so an out-of-scope target is a non-disclosing 404 on the read and the write alike, and
  every write is audited in the same transaction. The CLI reads `omniglass standard roles|set-role|delete-role`,
  `omniglass system roles|set-role|delete-role|assign-role|unassign-role`, and
  `omniglass component capabilities|set-capability|clear-capability`. The console follows the property pattern
  one tier up: a **Roles** editor on the standard blade (declare a role, set its quorum, pick the capabilities
  it requires), a **Roles** panel on the system detail (each role with its source, its staffing, an
  **understaffed** marker, and assign / unassign), and a **Capabilities** panel on the component detail
  (the resolved set, with the component's own additions and suppressions marked against its product's).
  The shipped **`meeting-room`** standard
  declares **`room-mic`** (microphone + speaker, quorum 2) and **`main-display`** (flat-panel-display, chosen so
  the shipped Samsung QM55 can actually fill it), **seeded if absent** on the operator-owned lane. Proven by the
  `EffectiveCapabilities` storage integration test (the product's set, an addition, a suppression, and the
  productless component resolving to exactly its own declarations), the `EffectiveRoles` and assignment test
  (both arms resolving, quorum 2 reading understaffed 2 then 1 after one assignment, the idempotent re-assign,
  the **named** shortfall on a display that provides neither microphone nor speaker, a productless component
  that declares what the role needs being staffed successfully, the unassign-twice miss, the unknown-role
  not-found, and the one-off system seeing only its own roles), the `/systems/{name}/roles` HTTP e2e (the
  inherited and ad-hoc rows, the 422 that names the gap, the out-of-scope non-disclosing 404) plus the
  seeded-standard e2e, and the seed regression that **retunes a seeded role's quorum, re-runs the seed, and
  asserts the retune survived**. This is **PR7** of the estate-model shift
  ([ADR-0049](/architecture/decisions/#adr-0049-the-system-role-capability-gated-staffing-and-the-resolved-capability-set)).
  No page badge moves: [core entities](/architecture/core-entities/) stays `Partial` (a role's **impact** on
  health, `system_member` composition, template pinning, the cross-owner cascade, and operational mode are still
  ahead) and [API](/architecture/api/) stays `Partial`.
