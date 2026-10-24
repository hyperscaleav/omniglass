---
title: "A derived value is a default that tracks until touched"
date: 2026-08-08
status: "Accepted"
summary: "A derivable value fills at create, tracks live while the platform holds the pen, freezes on the operator's first edit, and resumes tracking only on an explicit reset; `component.name_generated` ships `DEFAULT false`, not the epic's `DEFAULT true`, so no pre-existing operator-typed name is silently claimed by the platform"
---

- **Date:** 2026-08-08 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [storage](/architecture/storage/)
- **Decision:** when the platform can compute a value an operator would otherwise type, it follows one
  rule with four binding clauses, and none of the four make the value a constraint: a derived value can
  prefill a field but it can never cause a write elsewhere to be refused.

  1. **The platform fills.** A component's `name` is minted at create when an operator leaves it
     blank: `<component_type stem>-<n>`, the ordinal the smallest positive integer no sibling matching
     that stem already claims in the same placement scope, a sibling on a different stem never blocking
     it (`generateComponentName`, `internal/storage/namegen.go:129-168`).
  2. **A platform-owned value tracks its facts.** While `component.name_generated = true`, the name
     recomputes inside the same transaction as whatever changed its inputs: a `:move` to a new
     placement (`internal/storage/components.go:632-655`) or an ordinary product `PATCH` that
     reclassifies the component to a new type (`components.go:470-496`). Both ride the causing write's
     own audit event, old and new name in its payload; a platform-driven recompute is never itself a
     `:rename`.
  3. **The operator owns it on first touch.** `:rename` (`components.go:706-727`) clears
     `name_generated` to `false` unconditionally, whether or not the row was already operator-owned,
     and from that write the platform never recomputes the name again no matter how the facts move
     afterward.
  4. **The operator can hand it back.** `:resetName` (`components.go:744-774`, gated by the same
     `component:rename` token `:rename` uses, since both change the identifier) regenerates the name
     from the component's **current** type and placement and sets `name_generated = true` again,
     whether or not it already was.

  The contrapositive is the other half of the rule: a value the platform must own unconditionally (a
  health verdict, a resolved effective-tags set) is computed on every read, never stored as a default
  an operator could edit into a lie. The boundary between the two is whether the operator is allowed to
  disagree with the platform's answer.

- **Decision (the migration default deviates from the epic's own wording):**
  `component.name_generated boolean not null default false`
  (`db/migrations/20260808090000_names_scope_to_placement.sql:39-46`) ships `DEFAULT false`, not the
  `DEFAULT true` the epic issue specified. This is deliberate: every component row that exists before
  this column lands was operator-typed under the pre-#627 model, where no generator existed to have
  picked a name for it. `DEFAULT true` would hand the platform a pen it never earned over real,
  pre-existing operator data and let the very first `:move` against one of those rows silently rename
  it. The gateway writes the flag explicitly on every insert from Task 14 onward, `true` on a generated
  create and `false` on an operator-typed one; the column default describes only a row this migration
  found already sitting in the table, never a row created after it.

- **Context:** the same shape resolved independently twice on this branch before it was named as
  one principle, which is what promotes it from a per-feature habit to a rule to check new work
  against. A component's name (Task 14) is the built case above. `createIdentity`
  (`web/src/lib/entities.ts`) already ran clauses 2 and 3 client-side for a catalog entity, deriving a
  slug from a display name live and freezing it on first edit, before this decision gave the
  server-side pattern a name. The product classification floor
  ([ADR-0086](#adr-0086-the-product-classification-floor-and-the-kind-split)) is the contrapositive's
  own evidence: a silent schema default on `product.kind` let a mislabeled cloud service read as
  correct forever, exactly the failure clause 1's explicit fill and clause 3's no-silent-default guard
  against, which is why `kind` is required at create rather than defaulted. A system's own
  `location_id` (`internal/storage/systems.go`) is a live counterexample to clause 2, worth naming
  because a reader could otherwise assume every platform-computable field tracks live: it is authored
  at create and changed only through `:move` ([ADR-0088](#adr-0088-a-placement-change-is-an-authorization-act-so-a-move-is-its-own-verb)),
  never derived from `system_member` rows, so clause 2's live tracking is confirmed here as a per-field
  policy choice this decision's own build makes differently for a system's placement and a component's
  name, not a mandate every derived field must take. The boot-seed reconcile-and-delete carve-out
  (`choice_alternate`,
  [ADR-0087](#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability),
  [storage](/architecture/storage/#migrations-three-buckets-kept-separate)) is a related but distinct
  pattern, about which seeded rows a reboot may remove rather than who holds the pen on an
  operator-facing value; it is cross-referenced here, not restated.
- **Tracked under** epic [#627](https://github.com/hyperscaleav/omniglass/issues/627).
