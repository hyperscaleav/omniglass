# Create-as-route for inventory (slice 1 of the create/edit unification)

Status: approved, building. Date: 2026-07-14. Route and blade mechanics verified against the code.

## Problem

Creating a component, system, or location returns you to the list; setting a tag then means
find the row, reopen it, switch to edit. `TagAdder` also renders in **view** mode, so a
read-only surface carries a write control. Bindings (tags, secrets, grants) are separate
resources keyed to the entity id and cannot be set until it exists, so binding is always a
post-create act; it belongs behind edit, not in view.

## The model

The detail surface is one accordion of ordered sections (Identity, Placement, Tags,
Secrets/Variables), rendered on both the docked blade and the addressable full page. Two rules:

- **View reads, edit writes.** No in-body binding or field mutation control renders in view.
- **Create is a route.** `New <entity>` navigates to `/<entity>/create`, a draft accordion:
  Identity and Placement are writable, the binding sections are present but locked until the
  row exists. Save commits the row and **navigates to `/<entity>/<id>`**, the same accordion
  now backed by a real id, in edit mode, binding sections live. No drawer opens or closes; the
  draft is deep-linkable like any page.

`/<entity>/create` is a static route that ranks above `/<entity>/:name` in the Solid router, so
`create` is a reserved segment (an entity literally named `create` is unreachable by URL, an
accepted edge). This deletes the drawer-then-reopen dance and the cross-surface hand-off that a
drawer create would need.

Slicing: this slice does inventory (Components, Systems, Locations). Slice 2 extracts a shared
form shell; slice 3 moves Users onto it. Both out of scope here.

## Mechanics (verified against the code)

`TreeList` today: `openCreate` opens a `Drawer` hosting `FormBody` (a full `<form>` with its own
`DrawerFooter` + `p.close()`); the full-page detail is driven by `focus: () => params.name`
resolving a node from the shared `index()`; `onOpenNode` navigates to `/<entity>/<id>`; edit is
the drawer reopened. The blade body (`EntityBladeBody`) calls `useBladeEdit` and is the sole
`renderDetail` caller under a `BladeEditContext` provider; the **full page** renders the same
`renderDetail` outside any provider, so `useBladeEdit` must not be called inside `renderDetail`.

Slice 1 changes:

1. **Draft-create route.** `New` navigates to `/<entity>/create`. The page treats
   `params.name === "create"` as a draft: render the accordion in a **draft mode** (Identity +
   Placement writable, a `Create` action, binding sections locked). On Save, call the create
   mutation, then `navigate('/<entity>/<newName>')`. The draft holds local signals, not an index
   node, so it is exempt from the blade-prune / index-resolution path.
2. **Edit state threads through `ListCtx`.** Add `editing()` plus a bound edit handle to
   `ListCtx`, sourced from the blade's `createEditSlot` on `ctxBlade` and from a **new edit slot
   wrapped around `FullPage`'s `renderDetail`** on `ctxFull`. `renderDetail` reads `ctx.editing()`;
   it never calls `useBladeEdit`. Both surfaces support edit.
3. **Accordion detail.** `renderDetail` renders ordered sections; view shows read-only facts and
   read-only **direct-binding** tag chips (`listEntityTags`, not the resolved `effective_tags`),
   edit shows the own-field inputs and `TagAdder` live, gated on `ctx.editing()`.
4. **Own-fields form, footerless in edit.** `FormBody`'s field blocks are reused in the accordion
   edit; its `DrawerFooter` and self-submit are suppressed in that mode, its commit bound to the
   edit slot's Save. The draft-create route reuses the same field blocks with a `Create` action.
   The create `Drawer` is retired for these pages.
5. **`openEdit` callers**: blade footer pencil begins edit on the blade slot; list-row pencil
   navigates to `/<entity>/<id>` then begins edit (a pending-edit flag consumed once when the
   node resolves, the Users `openPrincipalInEdit` pattern); full-page Edit begins edit on the
   full-page slot.

## The invariant, precisely

No in-body binding or field mutation control renders while `editing()` is false: no tag
add/remove input, no own-field `<input>`. Exempt: the footer mode-switch (Edit) and lifecycle
(Delete / Disable) chrome, and the read-only `EffectiveSecrets` / `EffectiveVariables` panels.

## Commit semantics

Own-field edits commit on Save and revert on Cancel. `TagAdder` keeps its immediate per-binding
write; Cancel reverts own-field edits only, not tag bindings, so the tag control is visually
separate from the Save/Cancel own-fields form. On the draft route, Save is the create commit;
tag/secret sections unlock only after it (they need the id).

## Testing (test-first)

New per-page test files (following `Users.test.tsx`; no shared harness). The mock returns the
created entity in the subsequent list GET (the blade resolves from the list `index()`).

- `New` navigates to `/<entity>/create`; the draft accordion renders with Identity/Placement
  writable and the binding sections locked.
- Draft Save creates the entity and navigates to `/<entity>/<newName>` in edit mode.
- `TagAdder` is absent in view, present in edit; view shows read-only direct-binding chips.
- No own-field input or tag control renders in view (footer Edit/Delete exempt).
- Own-field edit commits on Save, reverts on Cancel.

Existing `descriptors.test.ts` and nav tests stay green.

## Non-goals

Shared cross-page form shell (slice 2); Users (slice 3); secret/variable binding editors (not
built); interfaces/nodes/tasks; batching tag writes into Save; a `create` route for non-inventory.

## Ripple

`web/src/index.tsx` (three `/<entity>/create` routes); `web/src/components/TreeList.tsx` (edit
slot on `ListCtx`; full-page edit slot; draft-create mode; accordion `renderDetail`; retire the
create/edit Drawer; `openEdit` callers; pending-edit consume effect); `web/src/pages/{Components,
Systems,Locations}.tsx` (draft-create config, accordion view/edit split, `TagAdder` gating,
read-only chip view, `FormBody` footerless mode, `New` navigates to `/create`); `web/src/lib/*`
(pending-edit flag helper, or shared in `TreeList`). No API/CLI/gen/migration. Docs: the three
operator guides gain a note that create is a route landing in edit; `status.mdx` entry; a
decision-log ADR (a built page's interaction model changes).
