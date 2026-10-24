---
title: "A select over a loaded collection binds through a ref, not a value prop"
date: 2026-08-27
status: "Accepted"
summary: "A `<select>` whose options come from a collection the server answers for takes its value from `bindSelectValue(value, ...options)` (`web/src/lib/selectvalue.ts`) used as the element's `ref`, never from a `value=` prop: the control keeps no value it has no option for, and a value binding does not re-run when the OPTIONS are what arrived. Thirteen controls convert, the workspace Configure face's four among them. Two shapes stay on `value=` and the exemption is deliberate: a hard-coded or generated option list has no async gap, and a control whose value starts empty and only moves because the operator moved it has nothing stored to lose"
---

- **Date:** 2026-08-27 | **Status:** Accepted | **Pages:** [design system](/contributing/design-system/),
  [UI](/architecture/ui/)
- **Decision:** A `<select>` whose options come from a collection the server answers for carries no
  `value=` prop. It takes its value from **`bindSelectValue(value, ...options)`**
  (`web/src/lib/selectvalue.ts`), used as the element's `ref`: the returned callback creates an
  effect that reads every option source and then assigns `el.value`, so the value is re-applied on
  whichever of the two inputs arrives second. The effect is created inside the ref callback, so it
  belongs to the owner that rendered the control and is disposed with the edit face rather than
  outliving it, and it runs after the `<For>` that fills the control because Solid flushes user
  effects after the render effects that insert the options. The signature requires at least one
  option source, since a binding that tracks none is the defect itself.
  Thirteen controls convert, eight of them pickers that carry the fix to every consumer at once:
  `TreeSelect` (the parent and owner picker on Locations, Systems, Components, Variables and
  Secrets), `ComponentTypeSelect`, `SystemTypeSelect`, the interface blade's node picker, the
  product form's vendor and driver pickers, the parent-standard picker, and the command-type
  target picker, which passes **both** of its catalogs. The other five are page-local: the node
  placement, and the workspace Configure face's four (system type, standard, location type, and
  the location's parent mover, whose option pool is a memo over two queries and is passed as the
  tracked source itself).
  **Two shapes are exempt, and the exemption is the decision.** A select over a hard-coded or
  generated option list (a kind enum, a settings field's `enum` constraint) has no async gap to lose
  a value in. A select whose value starts empty and only ever moves because the operator moved it (a
  create form, an add-or-assign picker) has nothing stored to lose, and the placeholder its fallback
  would land on is the value it already holds. Thirty-six of the console's forty-nine select sites
  are one of those two, so a blanket conversion would have been as wrong as a per-page patch.
- **Context:** A native `<select>` holds no memory of a value it has no `<option>` for: assign one
  and the control keeps nothing, and when the options arrive the browser's selectedness algorithm
  picks the first one instead, silently. Every face that deep-links into edit has the ingredients,
  because the stored value is known as soon as the entity resolves while the options come from a
  separate query. Either order is possible, so the defect presents as a flake rather than a bug, and
  an operator who saved in that window saved the fallback: on the Configure face's location type,
  whose select carries no placeholder option, the fallback is not even empty, it is the first type
  in the catalog, so the save silently retypes the location.
  A `value=` binding cannot answer it. Solid re-runs that binding when the **value** changes, and in
  the losing order the value never changes, the options do. Holding the control back behind a
  `<Show>` until its collection settles was the alternative and was rejected: it makes every picker
  on every edit face flicker between a fact and a control on a slow answer, and it would have to be
  repeated at each of the thirteen sites, which is the per-page patch this decision exists to avoid.
  It survived until a screenshot gate with no tolerance made it reproducible: the
  `entity-edit-face` shot flipped between two location types across captures
  ([ADR-0121](#adr-0121-the-console-ships-its-own-typefaces) landed the gate that caught it)
  (#398, #772, #782).
