---
title: "A component, system, or location's resolved path, its renders, and the console's own addressing all move to uuid"
date: 2026-08-08
refs: []
form: bullet
---

- **A component, system, or location's resolved path, its renders, and the console's own addressing
  all move to uuid** ([#627](https://github.com/hyperscaleav/omniglass/issues/627) Task 15, `c381060`,
  `cc8918b`, `280f95f`, `cb164ad`). A `GET` / `LIST` response now carries `path`, `path_segments`, and
  two display-only `renders` (`dash`, the accessor stripped; `bare`, the final segment further
  compacted to the component's `component_type.abbrev`), computed by walking the entity's own
  placement, never a system it happens to belong to; a `LIST` skips the extra queries the bare render's
  abbrev needs, since no console surface reads it there. The console's URL, every panel, and every
  write on a component, system, and location detail page now address by `id`, ending the bare-name
  addressing this epic exists to retire, with a disambiguation chooser offered when a search matches
  more than one same-named row. A review pass found the chooser pointed at a dead end: every panel on
  the detail page it opened still addressed by name, so the exact case the epic legalizes, two
  same-named components in different rooms, landed an operator on a page where every write `409`d
  (`bfdd7c8`); traced and fixed panel by panel to the wire. A second review pass found three more
  swapped-value consumers the edit sites had not been traced past (a raw uuid painted in a field
  labelled "Component", a reachability-panel cache key still built from the old name, a systems-list
  health badge still keyed on name against panels now invalidating by uuid), the fourth time on this
  branch that enumerating consumers from the edit site rather than the value's type and usage missed a
  real one (`2330954`, `01e4693`). Fixing the assign and add pickers to submit a uuid, above, closed
  staffing but left an asymmetry open on the way out: unassigning a role or removing a member still
  addressed the component by name through `UnassignRole` / `resolveMembershipEnds`, both resolving
  estate-wide via `scopedByName`, so an operator could staff a role or add a member with a
  duplicate-named component and then not undo it. A new `loadByRefWithin` primitive resolves within the
  relation actually being edited (this role's occupants, this system's members) instead of the whole
  estate, closing the gap with no wire change (`5a97266`, closing
  [#645](https://github.com/hyperscaleav/omniglass/issues/645)). `render_test.go`'s ten cases were
  found to exercise a test-local reimplementation of the segment shape rather than `PathOf` itself, the
  fourth occurrence on this branch of a test built on a reimplementation of the thing it is meant to
  catch; rewritten to assert against real fixture rows (`79c2ccb`).
