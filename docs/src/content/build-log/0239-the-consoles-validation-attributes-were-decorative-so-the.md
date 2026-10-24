---
title: "The console's validation attributes were decorative, so the rule moved to TypeScript"
date: 2026-08-14
refs: []
form: bullet
---

- **The console's validation attributes were decorative, so the rule moved to TypeScript**
  ([#724](https://github.com/hyperscaleav/omniglass/issues/724)). Found while closing
  [#718](https://github.com/hyperscaleav/omniglass/issues/718), whose brief asked for a `min="0"` on
  an input that had carried one since [#411](https://github.com/hyperscaleav/omniglass/issues/411)
  and had never fired once. The browser enforces `required`, `min`, `max` and `pattern` on a real form
  **submission**, and this console performs none on the paths an operator uses: a Drawer's action rail
  is drawn by the shell and portaled outside the `<form>`
  ([ADR-0054](/architecture/decisions/#adr-0054-the-shell-owns-a-panels-action-rail-the-body-registers-and-never-draws)),
  a blade has no `<form>` at all, and the inline editors save from an `onClick`.

  **The audit decided the ruling rather than following it**: 21 attributes on 24 rendered controls,
  17 sites on a surface with no form submission, and the remaining four (three on Login, one on the
  forced password change) sitting in forms whose submit button is `disabled` in exactly the states
  native validation would have refused. **Zero of the twenty-four could ever fire**, so a reader could
  not tell a live attribute from a decorative one because there were none of the first kind.

  So the rule is one vocabulary
  ([ADR-0113](/architecture/decisions/#adr-0113-a-validation-rule-is-typescript-and-a-native-constraint-attribute-is-not-one)):
  a pure function judges the typed value, the surface renders its message inline beside the field, and
  the binding's `disabled` / `valid` refuses the submit, which reads the same on a form and on a blade.
  Making the attributes live instead (`form.requestSubmit()` from the rail) would have covered the
  Drawers only, left every blade needing this decision anyway, and meant undoing the disabled gate so
  an unstyled browser bubble could refuse in place of the inline message.

  One rule was genuinely missing rather than duplicated. The token drawer's `min="1" max="365"` had no
  TypeScript behind it, so a 400-day token was refused by the server's 422 after a round trip; it is
  `tokenTtlError` now, asserted through the drawer's own rail button rather than by calling the
  validator. `aria-required` replaces `required` on the eleven fields that carried it, since a screen
  reader should still hear the fact; `type="email"` and `type="number"` stay, because an input type
  carries keyboard, autofill and spinner behaviour and is not a claim about refusal.
  `web/src/validation-guard.test.ts` scans every `.tsx` and fails on a native constraint, with a
  self-check that the scanner can still find one (its first version could not: a JSX attribute written
  after an arrow-function handler defeats any regex that stops at the first `>`).
