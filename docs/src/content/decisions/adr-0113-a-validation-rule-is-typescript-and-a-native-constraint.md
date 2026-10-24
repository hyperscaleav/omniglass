---
title: "A validation rule is TypeScript, and a native constraint attribute is not one"
date: 2026-08-13
status: "Accepted"
summary: "A console control carries **no** `required`, `min`, `max`, `pattern` or `step`: a rule is a pure function over the typed value, the surface renders its message inline beside the field, and the binding's `disabled` / `valid` refuses the submit. The audit decided it: 21 attributes on 24 rendered controls and **zero could ever fire**, because a Drawer's rail is portaled outside the `<form>` (ADR-0054), a blade has no form at all, and the four on genuine form paths sit in forms whose submit is disabled in exactly the states native validation would refuse. Wiring `form.requestSubmit()` instead would have covered the Drawers only, left every blade needing this decision anyway, and meant undoing the disabled gate so an unstyled browser bubble could refuse in place of an inline message. `aria-required` stays as the honest spelling, `type=\"email\"` and `type=\"number\"` stay as input types, and a guard test scans every `.tsx`"
---

- **Date:** 2026-08-13 | **Status:** Accepted | **Pages:** [design system](/contributing/design-system/)
- **Decision:** a console control carries **no** `required`, `min`, `max`, `minlength`, `maxlength`,
  `pattern` or `step`. A rule is a **pure function** over the typed value (`lib/validate.ts`,
  `readSettleWindow` in `lib/command_types.ts`), the surface renders its message **inline beside the
  field**, and the form binding's `disabled` / `valid` refuses the submit. `aria-required` stays,
  because it announces a fact to a screen reader without claiming the browser will refuse the value.
  `validation-guard.test.ts` scans every `.tsx` for a native constraint on an `input`, `select` or
  `textarea`.
- **Context:** the browser enforces a constraint attribute on a real form **submission**, and this
  console performs none on the paths an operator uses. A Drawer's action rail is drawn by the shell
  and portaled outside the `<form>` ([ADR-0054](#adr-0054-the-shell-owns-a-panels-action-rail-the-body-registers-and-never-draws)),
  a blade has no `<form>` at all, and the inline editors (RoleEditor, ContractEditor) save from an
  `onClick`. **The audit is what decided it:** 21 attributes on 24 rendered controls, of which 17
  sites are on a surface with no form submission at all, and the remaining four (three on Login, one
  on the forced password change) sit in forms whose submit button is `disabled` in exactly the states
  native validation would have refused. **Zero of the twenty-four could ever fire.** A reader could
  not tell a live attribute from a decorative one because there were none of the first kind.
- **Why not make them live instead (`form.requestSubmit()` from the rail):** it would have covered
  the Drawers and left every blade needing this decision anyway, since a blade is a field set with a
  contributor registry rather than a form, so native validation could not be the one vocabulary. It
  would also mean threading a form ref through the binding into a portaled shell to reproduce an
  effect `disabled` / `valid` already produce, and then **undoing** the disabled gate so the browser
  could refuse instead, which is a worse operator experience: a native bubble is unstyled browser
  chrome that says less than the inline message and vanishes on blur.
- **What it costs and what it does not:** every removed attribute already had a TypeScript rule
  behind it except one, the token drawer's `min="1" max="365"`, which is now `tokenTtlError` and
  refuses a 400-day token at the field rather than at the server's 422. `type="email"` and
  `type="number"` stay: an input TYPE carries keyboard, autofill and spinner behaviour, and is not a
  claim that a value will be refused.
- **Found by** [#718](https://github.com/hyperscaleav/omniglass/issues/718), whose brief asked for a
  `min="0"` on an input that had carried one since [#411](https://github.com/hyperscaleav/omniglass/issues/411)
  and had never fired once. **Tracked under** [#724](https://github.com/hyperscaleav/omniglass/issues/724).
