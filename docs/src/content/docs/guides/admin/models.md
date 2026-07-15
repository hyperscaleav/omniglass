---
title: Models
description: "The Models registry: the make + model product catalog (make, model number, family, lifecycle, front/back photos) behind future component classification, seed-owned official rows read-only, admin-gated custom ones."
---

**Catalog > Models** (`/component-models`, with `model:read`, covered by every viewer's `*:read`
floor) is the directory of **component models**: specific device products (a Crestron Flex MX70, a
Biamp Tesira Forte AI), one layer down from [Makes](/guides/admin/makes/). It is the primary catalog
browse surface; Makes stays the manufacturer-admin peer beside it. Each row shows the **id**, the
**display name**, a **Make** column (the manufacturer it belongs to), the **model number**, and its
**origin** (**official**, seed-owned, or **custom**). A **make filter** narrows the directory to one
manufacturer.

A model references a [component make](/guides/admin/makes/): everything a component product needs to
be named and looked up (who makes it, what it is called, its model number, an optional **family**
grouping, and optional **lifecycle** dates: released, end-of-sale, end-of-life). It carries **no
type or classification field** in this slice; what a model *does* (mic, speaker, codec, switcher, and
so on, an all-at-once multi-valued **roles** model, not a single genus) is a later catalog slice, not
built yet. A `component_type` genus tree and stamping a model onto a `component` instance are also
later work. See [core entities](/architecture/core-entities/) for where the model registry sits in the
estate model.

- **New model** (with `model:create`, an admin permission) opens a create drawer: name its **id** (a
  short identifier, unique tenant-wide, e.g. `tsw-1070`), give it a **display name**, pick its **make**
  (required, fixed after creation), and set a **model number**. **Family**, the three lifecycle dates,
  and front/back **images** are optional.
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `model:update`) edits the
  display name, model number, family, lifecycle dates, and the front/back images; the id and the make
  are fixed. **Delete** (with `model:delete`) removes the row, behind a confirm.
- An **official** (seed-owned) row is always read-only: no Edit, no Delete, and the blade marks it
  "Seed-owned, read-only." This slice ships no official models (a boot-seed product catalog is later
  work); a dev-only seed installs a few example models (referencing the seeded makes) so `make dev`
  comes up with the Models page populated.
- **Front and back images** are product photos. Uploading one goes through the same [Files](/guides/admin/files/)
  primitive the rest of the console uses: pick an image, it uploads to the files store, and the
  returned file id is set on the model. The blade renders whichever images are set and shows a plain
  placeholder where one is missing.
- **Delete carries the make-in-use guard.** `component_model` is the first entity to reference
  `component_make`, so deleting a make that a model still points at is now refused (409) from the
  [Makes](/guides/admin/makes/) page or CLI, the same delete-refused-while-referenced rule the
  [Types](/guides/admin/types/) registry already enforces. Deleting a model that nothing references
  (nothing does yet) is unconditional (still refused for an official row, 422).

Minting a model is admin-gated. The same operations are `omniglass component-model
list/get/create/update/delete` from the CLI (see the [CLI reference](/reference/cli/)).
