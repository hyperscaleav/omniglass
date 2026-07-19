# Field rendering: the override model, as one generic primitive

Design mock (approved): the `Field rendering: the override model` artifact.

## Why

A component field renders as a bare value seeded into an input, where typing implies override.
This breaks three ways:

- **Bool has no inherited state.** A toggle is always on or off, so an inherited `default=true`
  renders as a value you appear to have set.
- **Override is not a choice.** Inherited vs overridden is inferred from an empty box, not chosen.
- **Sourced values do not fit.** Once a field can resolve from a secret or variable, "the value" is
  not a literal you type over, and the stored `$sec:name` is meaningless to a non-programmer.

And the renderer is ad-hoc: the same field shape is hand-built per surface. It should be **one
generic primitive** consumed everywhere a resolved-value-with-override appears.

## The primitive

`FieldControl` (working name): renders one field's **resolved value** and its **override** state,
in two modes. It is the field-facing sibling of `KVRow` (slim value row) and `KVStacked` (stacked
label/value), composed for the override interaction. Every field-like renderer converges on it.

- **Read (slim, one line):** label left, resolved value right (value column stays scannable). An
  override reads with an accent **dot on the key** and the value in the **accent color**, together.
  Inherited reads muted, no dot.
- **Edit (stacked, two rows):** key row = label + a right-aligned **Override** switch (fixed
  position, aligned down the column regardless of key/value length). Value row = the resolved value
  (muted) when the switch is off, or the override input when on. **Revert is the switch off**, there
  is no separate control.

## What the switch encodes

- **Off (inherit):** the field resolves to whatever the cascade gives (see below), shown muted with
  a provenance chip. No editable input exists.
- **On (override):** an input appears, seeded from the resolved value; the row now reads as the
  operator's own value.

## Per type

- **string / int / float / json:** a type-aware input in the override row (json = textarea).
- **bool:** inherited = the resolved word (`true`/`false`) muted; override on = a real editable
  toggle on the value row. This is the case the model exists to fix.

## Required

- A red **`*`** by the key marks a required field, always.
- The red input box is reserved for **submit validation**: only on a save attempt with the field
  empty does the box go red and a "This value is required" label appear beneath it (standard form
  behavior). Save is blocked while any required field is unfilled (the blade `valid()` gate).
- A required field forces override on and cannot be switched off until it has a value.
- `field_definition` gains a `required boolean` (default false).

## Sourced values (drawn now, wired later)

A source-capable field can resolve from a secret/variable/file. The rendering is designed now so it
does not surprise us; the mechanism to set a source rides the deferred field-sources model.

- **The `$` picker:** a single fixed-slot trigger inside the field's right edge (no wrap), faint at
  rest, that opens an **overlay** menu (Secret / Variable / File) without reflowing the field. `$` is
  the interpolation sigil.
- **A sourced value renders as symbol + name**, never the raw `$sec:name` (that string is DB storage,
  resolved at run, meaningless to the operator). A secret shows `<key> name` with no value; a variable
  `<var> name`. Symbols: secret = key, file = page, variable = an open pick (matched SVGs in build,
  emoji are placeholder).

## The cascade and the type default (decision, ADR)

Raised during design: does a value set higher in the cascade beat the type default? **Yes, and it
does not bend the cascade.** The **type default is the floor of the cascade**, not a competitor to
it. Resolution is deepest-set-wins down `product -> location -> system -> component`; if nothing is
set at any scope, the value falls to the field's type default. This lands as an **ADR** in the
decision log, and the multi-scope cascade itself is a **later issue** (slice 1 is component-only:
resolved = this component's set value, else the type default).

## Scope: one slice, all of field rendering

- **Build now:** `FieldControl` (read slim + edit stacked), the Override switch and revert, dot +
  accent-color read signal, all literal types (string/int/float/bool/json) including the bool
  toggle, required (marker + submit validation), and `field_definition.required`. `EffectiveFields`
  becomes the first consumer.
- **Draw now, wire later:** the `$` picker and the symbol-plus-name sourced display. Present in the
  primitive, non-functional until the sources backend lands (a `field_definition` source-capability
  flag and a `field_value` that can hold a source reference are the later increments).

## Generic-primitive mandate

`FieldControl` is a platform primitive, not a component-detail helper. A **follow-on sweep** roots
out every hand-built field renderer and replaces it with it, and a later slice brings fields (and so
this control) to the other entity drawers, not just components.

## Data model

- `field_definition.required boolean not null default false` (additive migration).
- `field_value` unchanged this slice (a literal). The source-reference value is a later increment.
- No change to the effective read shape beyond carrying `required` through.

## Testing

- Storage/API: `required` round-trips on define/update; the effective read carries it.
- Web: `FieldControl` unit tests for read (dot + color, inherited muted), edit (switch off = value,
  on = input; revert), bool (inherited word vs editable toggle), required (marker always; red box +
  label only after a submit attempt; Save gated). The converted `EffectiveFields` still batches on
  the blade save.
- Live screenshots: the edit panel across types, a required-unfilled submit, read mode with an
  override. Seed a bool and a required field so both render.
