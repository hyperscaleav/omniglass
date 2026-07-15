# Component make/model catalog (lightweight cut)

A normalized catalog layer between the coarse `component_type` classifier and the versioned
`component_template`: who makes a device (`component_make`), what product it is
(`component_model`), and what genus of device it is (`component_type` as a tree). A component
instance points at its model and inherits its classification.

This is the inventory-identity slice of a larger idea. It ships alone and delivers fleet
inventory and product identity with no dependency on the parts still in design.

## Goal

Represent make and model as first-class, normalized, seed-and-custom catalog data, so an
operator can say "this component is an Acme 123A, made by Acme," answer "how many Acme 123A
across the estate," and classify devices by genus (display / dsp / microphone), not just the
coarse substrate (device / app / cloud-service).

## Non-goals (deferred to their own epics)

- **Slots / card compatibility** (`installs_into`, chassis bays). Containment-constraint,
  separable, later.
- **model to template stamping** (`default_template`, pin defaulting). Templates are `Design`
  status, unbuilt; there is nothing to default to yet. Identity now, stamping when templates land.
- **Ports / links / connections / maps.** The connectivity primitive is in active design and
  reframed as an observation ("Zabbix maps") model; it does not gate this cut.

## Ontology

| Layer | Entity | Example | Status |
|---|---|---|---|
| substrate | `component_type` root | device / app / cloud-service | exists, coarse |
| genus | `component_type` leaf | display / dsp / microphone / amplifier / switcher | new (tree) |
| make | `component_make` | Acme (icon, support#, website) | new |
| product | `component_model` | Acme 123A (make, model#, family, genus, EOL) | new |
| instance | `component` | `dsp-boardroom-3` -> model | exists, `+model_id` |

### Rules

- **Classification lives on the model; the instance inherits.** A component's substrate and
  genus come from its `model`. "All Acme 123A are DSPs" is true by construction, no per-instance
  retyping.
- **`component_type` becomes a tree.** Add `parent_id`; substrate rows are roots, genus rows are
  their children; a model points at a leaf; the substrate derives by walking to the root. Only
  the component kind gets the tree this cut (location / system / secret types stay flat).
- **`component.component_type` stays as a nullable fallback** for model-less (ad-hoc, not yet
  cataloged) components; when a model is set, classification derives from it. Migration nulls
  nothing; back-compatible.
- **A model needs no template.** Model is identity; a component still classifies and inventories
  with no template in sight. Template-stamping is a later epic.
- **Origin `official | seed | custom`** on make and model, matching the type registry: seeded
  rows are read-only, so a shared baseline (common makes) cannot drift install to install.

## Slices (one vertical PR each)

1. **`component_make` registry.** New entity: name, display_name, icon (glyph key), support_phone,
   website, origin. Scoped CRUD API + generated CLI + client, a Catalog surface (its own page or a
   Catalog tab), seed a baseline set of official makes. Delete refused (409) while a model references
   it; official rows read-only (422). No deps.
2. **`component_type` to tree.** Add `parent_id` to the type registry (component kind), seed genus
   leaves under the substrate roots, Component tab renders the tree, picker offers leaves. Delete
   refused while a child or a referencing model remains. No deps.
3. **`component_model` catalog.** New entity: name, display_name, make (FK), model_number, family,
   type (FK to a `component_type` genus leaf), lifecycle (released / eos / eol dates), origin, and
   **front / back product images** (nullable file refs, reusing the files primitive from #246: a
   photo is a property of the product, not of any instance or template). CRUD + CLI + client + a
   Catalog surface. Depends on 1, 2.
4. **`component.model_id`.** Add the nullable FK; the create/edit form picks a model; the console
   shows derived make + genus; `component_type` derives from `model.type` with the on-row column as
   fallback. Depends on 3.

Slices 1 and 2 are independent (either first). 3 is the hub. 4 wires instances in.

## Data model sketch

```
component_make       id, name (unique), display_name, icon, support_phone, website, origin, ts
component_type       + parent_id (self-ref, nullable; roots = substrate, leaves = genus)
component_model      id, name (unique), display_name, make_id -> component_make,
                     model_number, family, type_id -> component_type (a leaf),
                     released_at, eos_at, eol_at, origin,
                     front_image_id / back_image_id -> file (nullable, #246 files), ts
component            + model_id -> component_model (nullable)
```

All migrations idempotent dbmate DDL; new reference rows seeded via the boot seed phase
(`ON CONFLICT DO UPDATE`), never in a schema migration. Gateway ripple + scoped reads per the
storage doctrine.

## Docs and status

Each slice ships its docs in the same PR: a Catalog guide page for makes and models, the
`component_type` tree note on the types page, `core-entities.md` gains the model layer and the
`component.model_id` pointer, status badges advance to their new floor, `status.mdx` build note,
and a decision-log entry for the classification-on-model rule and the two deferrals.

## Forward hooks (do not build here, do not preclude)

- **Slots epic:** `component_model.slots` (chassis bays) + a card's `slot_type` / `installs_into`,
  with attach-time constraint. Rides the containment tree.
- **Template stamping:** `component_model.default_template`, pin defaulting on assign, when the
  template entity is built.
- **Connectivity ("maps") epic:** the observation graph (entity + connection edge + datapoint
  binding, rendered as a map). Separate, in design. A model may later declare named connection
  anchors (points), but this cut adds none.

## Testing

Unit: registry validation (unique names, kebab ids, cycle-safe `parent_id`, official-row
read-only). Integration (testcontainer): make/model/type CRUD round-trips, delete-refused-while-
referenced (409), classification derivation from `model.type`. E2E: API + CLI + UI create/read of
a make, a genus, a model, and a component pointed at a model showing derived genus.
