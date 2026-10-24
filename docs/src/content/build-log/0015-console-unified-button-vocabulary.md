---
title: "Console: unified button vocabulary"
date: 2026-07-06
refs: []
form: bullet
---

- **Console: unified button vocabulary.** Buttons across the console moved from ad-hoc daisyUI classes
  (`btn-primary`, `btn-ghost text-error`, and an inconsistent disable/edit pairing) to a small set of
  **semantic intent classes** defined in `app.css` (`btn-action`, `btn-quiet`, `btn-danger`, `btn-warn`,
  `btn-ok`), `@apply`-composed from the daisyUI theme tokens so a future custom theme restyles every
  button from one place. A `style-guard` unit test scans the source and fails on a raw `btn-primary` /
  `btn-ghost`, pinning the vocabulary against drift. No behavior or API change: purely how buttons are
  styled. See the [design system](/contributing/design-system/#button-vocabulary). Follow-on: custom
  theme switching beyond the two brand themes.
