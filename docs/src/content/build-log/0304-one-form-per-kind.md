---
title: "One form per kind"
date: 2026-10-04
refs: []
form: bullet
---

- **One form per kind**
  ([#826](https://github.com/hyperscaleav/omniglass/issues/826) slice 1, #830). The
  original blade vision, restored: `EntityForm` is the one component that renders a
  location, a system, or a component read or edit, and whether it appears in the blade
  or on the workspace's Configure tab is only where the operator clicked. The host owns
  the edit slot (the blade's footer, the page's footer) and hands it in; the form binds
  seed and save, keeps every field's own gate (a caller with update but not rename sees
  the name read-only, with the reason), wires its kind panels through the ambient blade
  stack, and keeps the ruled save order (update, move, rename last, uuid-addressed).
  Create is the same form empty (`EntityCreateForm`: what and where before identity,
  the platform-named identity fields locked on the value they will carry, and an
  `under` prefill for the explorer's create-where-you-stand), so the three create pages
  become hosts that only say where to go next. The #800 jump-anchor rows retire; the
  blade's members, strip, and vitals move to the workspace they summarised.
