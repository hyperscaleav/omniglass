---
title: "The system role: capability-gated staffing and the resolved capability set"
date: 2026-07-21
status: "Superseded by [ADR-0087](#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability)"
summary: "A **`system_role`** is a slot a system needs filled (a table microphone, a main display), declared on a **standard** (inherited live by every conforming system) or on one **system** (ad-hoc) over the same exclusive arc `property_value` uses, requiring a **conjunctive** `role_capability` set and carrying a **`quorum`**. A component's capabilities become a **resolved set** (`EffectiveCapabilities` = its product's, plus its own `component_capability` `present=true` rows, minus its `present=false` ones), because `product` is optional and a strict guard over a product-only fact would lock a productless component out of every role. `AssignRole` **refuses (422) and names the missing capabilities**, joining the location placement constraint as a refusal on modeled grounds that names the parties. **Quorum** ships here (staffing is visible without health); **impact** and the SLI rollup land in PR8. Supersedes the `system_template_member` role-requirement design. PR7 of the estate-model shift"
---

- **Date:** 2026-07-21 | **Status:** Superseded by [ADR-0087](#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability) | **Pages:** [core entities](/architecture/core-entities/),
  [API](/architecture/api/), [glossary](/architecture/glossary/), [templates](/architecture/templates/),
  [health](/architecture/health/), [Standards guide](/guides/admin/standards/),
  [Capabilities guide](/guides/admin/capabilities/), [Work with an entity](/guides/operator/entities/)
- **Decision:** A system says **what it needs filled**, and the platform **refuses** a component that cannot
  fill it. Four tables and two resolvers carry that.

  **1. A `system_role` is a slot, declared on the arc.** A role (a table microphone, a main display) is
  declared either on a **`standard`**, where **every conforming system inherits it live**, or **directly on
  one `system`** (ad-hoc, which is how a one-off system gets roles at all). The two owners ride the **same
  exclusive-arc pattern `property_value` uses**: an `owner_kind` plus `standard_id` / `system_id`, a one-set
  CHECK, and a `unique nulls not distinct` key over the arc columns and the role name (the default NULLS
  DISTINCT would let duplicates through the NULL arm). A role carries a **`quorum`**: how many components
  should fill it, at least one, because a role no component need fill is not a role.

  **2. `role_capability` is conjunctive.** A role requires a set of [`capability`](#adr-0044-the-component-classification-catalogs)
  rows, and a component must provide **every** one of them. Requiring nothing admits anything, which is the
  honest reading of an empty requirement, not a special case.

  **3. A component's capabilities become a resolved set.** **`component_capability`**
  (`component_id`, `capability_id`, `present`) is the component's **own** capability facts, layered over its
  product's: `present=true` **adds** one the product does not claim, `present=false` **suppresses** one it
  does. **`EffectiveCapabilities(component)`** is then the product's set UNION the additions MINUS the
  suppressions, and a **productless component resolves to just its own declarations**. This is the single
  definition of "what this component can do" for the whole platform, and it is the set the guard checks.

  **4. `EffectiveRoles(system)` merges both arms.** The roles the system's standard declares (marked
  `from_standard`) UNION those declared directly on it, each with its required capabilities, its quorum, and
  the components filling it here. A one-off system has only the ad-hoc arm. The resolver serves `Assigned()`
  and `Understaffed()` (quorum minus assignments, floored at zero) rather than leaving arithmetic to each
  surface, so staffing reads the same way everywhere.

  **5. The guard refuses, and the refusal names the gap.** `AssignRole` is a **422 when the component's
  resolved capabilities do not cover every capability the role requires**, and the message names the missing
  ones (`component "panel-1" cannot fill role "table-mic": missing microphone, speaker`), sorted so the same
  gap always reads the same way. It joins the **location placement constraint** as a refusal on **modeled**
  grounds, and follows the same rule that one set: **name the parties**. A bare "no" leaves the operator
  nothing to do, and the whole value of modeling capability is that the refusal is actionable. Assignment is otherwise idempotent, and **`role_assignment.component_id` is
  `on delete restrict`**, so a component staffing a role cannot be deleted out from under the system.

  Eight routes carry it, regenerated into the OpenAPI document, the cobra CLI, and the typed client:
  `GET /standards/{id}/roles` plus `PUT` / `DELETE /standards/{id}/roles/{role}` (gated `standard:read` /
  `:update` / `:delete`); `GET /systems/{name}/roles` (the resolved read) plus
  `PUT` / `DELETE /systems/{name}/roles/{role}` and
  `PUT` / `DELETE /systems/{name}/roles/{role}/assignments/{component}` (gated `system:read` / `:update`);
  and `GET /components/{name}/capabilities` plus `PUT` / `DELETE /components/{name}/capabilities/{capability}`
  (gated `component:read` / `:update`). Every system and component route resolves its owner **within the
  caller's scope first**, so an out-of-scope target is a non-disclosing **404**. The shipped `meeting-room`
  standard declares `room-mic` (microphone + speaker, quorum 2) and `main-display` (flat-panel-display, chosen
  so the shipped Samsung QM55 can actually fill it), **seeded if absent** on the
  [operator-owned lane](#adr-0048-the-standard-blueprint-and-the-template-fork-seed-model), so an operator's
  quorum retune survives a re-seed.
- **Context:** The strict refusal was decided first: a role that names a requirement and then lets anything
  fill it is decoration. But a component's capabilities came **only from its product**, and `product` is
  deliberately **optional** on a component
  ([ADR-0047](#adr-0047-the-fields-fold-product_property-and-property_value)), so under a strict guard a
  **productless component could have filled no role at all**. Three ways out, and only one of them keeps both
  halves: make product mandatory (reverses a call made one slice ago for good reasons), soften the guard to a
  warning (throws away the point of the model), or let a **component declare its own capabilities over its
  product's**. Layering resolves the tension without touching either commitment, and it is not a new shape: it
  is exactly the **contract-plus-override** the declared properties already use, where a product declares a
  default and an instance overrides it, applied to capabilities instead of values. Quorum lands in this slice
  and impact does not, because **staffing is visible without health at all**: a role wanting 2 with 1 assigned
  is under-staffed today, on data the operator entered, with no engine reading anything.
- **Deferred:** **`impact`** (`outage` / `degraded` / `none`, what an unfilled or failing role does to its
  system) and the whole **SLI rollup**, which land in **PR8** with the engine that reads them; the console
  surfaces for both arcs (the standard's role editor, the system's roles panel, the component's capability
  editor); **role-scoped config**, a value declared against a role slot and resolving onto whichever component
  fills it ([templates](/architecture/templates/) describes it, nothing builds it); a **cap at quorum** (a role
  may be over-staffed, and nothing refuses the extra assignment, because "more than enough" is not an error);
  and the **`system_member`** composition table, which stays `Design`.
- **Supersedes:** the **`system_template_member` role-requirement** design on
  [templates](/architecture/templates/). That page still describes a role slot **frozen into a
  `system_template_version`**, whose requirement is a set of **canonical datapoints and commands** and whose
  instance assignment is a **`system_member`** row. What shipped puts the slot on the **standard / system
  arc** (a standard is the blueprint now, so the role belongs with it), states the requirement as a
  **capability** set (a coarser, operator-legible vocabulary that exists as a catalog today, where canonical
  commands do not), and records the assignment in **`role_assignment`**. Templates and their frozen BOM stay
  `Design`; the two models are reconciled when template pinning is built, and until then the built role model
  is the one on [core entities](/architecture/core-entities/). Also supersedes the reading, running through
  [ADR-0044](#adr-0044-the-component-classification-catalogs) and
  [ADR-0045](#adr-0045-the-product-catalog), that **a capability is a product-only fact**: a capability is now
  a fact about a **component**, which its product supplies a default for.
- **Tracked under** epic [#266](https://github.com/hyperscaleav/omniglass/issues/266). This is **PR7** of the
  estate-model shift toward property / event / command plus vendor / product / driver / capability / standard /
  role / health.
