---
title: "Field: the override model"
date: 2026-07-19
refs: []
form: bullet
---

- **Field: the override model.** Field rendering converges on one generic primitive, **`FieldControl`** (the
  field-facing sibling of the `KVRow` / `KVStacked` key:value pair), consumed everywhere a
  resolved-value-with-override appears. It renders in two modes. **Read** is a slim one-line row (label left,
  value right) where an **override** reads with an accent **dot on its key** and its value in the **accent
  colour**, while inherited stays muted. **Edit** is a stacked cell whose key row carries a right-aligned
  **Override** switch: switch **off** inherits the resolved value (the type default this slice) with no
  editable input; switch **on** reveals a type-aware input seeded from that value, and **revert is the switch
  off** (no separate clear). This fixes the **bool** case the old renderer got wrong: inherited, a bool shows
  the resolved word (`true` / `false`) muted rather than a toggle you appear to have set, and override on
  gives a real editable toggle. A new **`field_definition.required`** boolean (additive migration, default
  false, carried through the effective read) marks a required field with a red **`*`**, forces its override
  on, and gates the blade **Save**: the red input box and a "This value is required" label appear only after
  a submit attempt leaves the field empty, and Save is blocked while any required field is unfilled.
  **`EffectiveFields`** is the first consumer, now rendering every field through `FieldControl` and still
  batching each touched field onto the blade Save. The **`$` source picker** (variable / secret / file) and
  the symbol-plus-name display of a sourced value are **drawn in the control but wired in a later slice**;
  this slice sets literals only. Proven by `FieldControl` unit tests (read dot-and-colour and
  inherited-muted, edit switch-off-value versus switch-on-input and revert, the bool word-versus-toggle
  split, and the required marker plus the submit-only red box and Save gate), the `EffectiveFields`
  blade-batch test, and a storage/HTTP round-trip carrying `required`. The [config, secrets, and
  variables](/architecture/variables/) page keeps its `Partial` field badge.
