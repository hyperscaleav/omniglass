# component_model registry (slice 2 of the catalog epic #254, issue #257)

The product entity: a normalized make + model catalog. A `component_model` names a specific
product (an Acme 123A) made by a `component_make`, with lifecycle dates and front/back product
images. The console gains a **Models** directory (the primary catalog browse surface) with a make
column and a make filter; the shipped **Makes** page stays as the manufacturer-admin peer.

Builds on `component_make` (#255, merged) and the files primitive (#246, merged). No dependency
on the classification slice (#256).

## Goal

Let an operator catalog specific device products (make + model number + family + lifecycle +
photos), browse all models in one Models directory filtered by make, and manage each model behind
the same official-read-only and admin-gated rules as the make registry.

## Non-goals (deferred)

- **Classification / type / roles.** A model carries **no type field** this slice. Classification
  is being reframed (see #256) from a single genus to **multi-valued roles** (a device plays
  mic / speaker / codec / switcher / amplifier / control at once, like a presentation switcher);
  that is its own slice and does not block this one. When it lands, a model gains its roles with no
  rework here.
- **Slots / card composition** and **model to template stamping**: separate later epics.

## Ontology (this slice)

```
component_model
  id (kebab, unique) / display_name
  make_id        -> component_make        (required FK)
  model_number
  family                                   (optional grouping, e.g. "1xx-series")
  released_at / eos_at / eol_at            (optional lifecycle dates)
  front_image_id / back_image_id -> file   (nullable, reuse #246 files)
  official boolean                         (seed-owned rows read-only, mirrors component_make)
  created_at / updated_at
```

### Rules

- **Official rows read-only** (422 on update/delete), mirroring `component_make`'s `guardTypeMutable`.
- **Make in-use guard lands here.** `component_model` is the first entity to reference
  `component_make`, so deleting a make a model references now returns **409** (this is the
  referential guard deferred from #255). Deleting a model that nothing references is unguarded.
- **Images are files.** `front_image_id` / `back_image_id` are nullable FKs to `file` (#246). The
  console uploads an image through the existing files API (base64, the ADR-0018 avatar precedent),
  then sets the returned file id on the model. The model API only takes the FKs, so it reuses the
  files primitive verbatim and adds no new upload path.

## Surfaces

- **API:** `/component-models` (list/create/get/update/delete), a new `model:*` permission
  (`model:read` on the viewer read-floor, mutations admin-tier), official 422, make-in-use 409.
  Mirrors `component_make`'s handler shape and `mapTypeErr`.
- **CLI:** generated `omniglass component-model list|create|get|update|delete`.
- **UI:** Catalog > **Models** (new): a flat directory of all models with an **Id**, **Display
  name**, **Make** column, and **lifecycle** hint, a **make filter** (facet), create/edit/delete
  with **front/back image upload**, official rows read-only. The **Makes** page is unchanged (peer,
  under Catalog, for manufacturer detail). Nav gains a Models entry beside Makes.

## Seed

No boot seed of specific products (models are operator/device-pack territory, not ship-with
reference data). Add a few **dev-seed** example models (`internal/devseed/fixtures.yaml`)
referencing the seeded makes (e.g. a Crestron and a Biamp model), so `make dev` Models page comes
up populated and the make column is exercised. Idempotent.

## Slices (tasks)

1. **storage:** `component_model` table (make FK, image FKs, official) + Gateway CRUD + official
   guard, **plus the make-in-use delete guard** (deleting a referenced make -> 409). Testcontainer
   round-trip incl the FK guard.
2. **dev-seed:** example models referencing seeded makes.
3. **API + `model:*` permission + `make gen`:** 5 routes, viewer-floor read, admin mutations,
   official 422, make-in-use 409, regenerated client + CLI, drift clean. e2e.
4. **web Models page:** data layer + flat directory (make column + make filter) + create/edit/delete
   + front/back image upload (reuse the files upload flow) + official read-only + nav + route.
5. **docs + ship:** models guide, `component-models` API routes in api.md, CLI commands in cli.md,
   core-entities model-layer note, status.mdx, decision-log (no-type-this-slice, images-via-files,
   Models-primary IA, make-in-use-guard-lands-here). `/ship-slice` with live screenshots.

## Data model sketch

```
component_model   id, display_name, make_id -> component_make, model_number, family,
                  released_at, eos_at, eol_at, front_image_id/back_image_id -> file (nullable),
                  official boolean, created_at, updated_at
component_make    (delete now guarded: 409 while a component_model references it)
```

Idempotent dbmate migration. Gateway ripple + scoped reads per storage doctrine. `make gen`
regenerates the client + CLI from the Huma structs.

## Testing

Unit: model validation (unique id, make required, image-fk optional, official read-only). Integration
(testcontainer): model CRUD, make-in-use 409, official 422, image-fk round-trip. E2E: API + CLI + UI
create/read of a model with a make and an uploaded image, the Models directory filtered by make.
