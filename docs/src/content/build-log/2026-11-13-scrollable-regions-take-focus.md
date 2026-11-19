---
title: "A scrollable region takes a focus stop (#1101)"
date: 2026-11-13
refs: [1101, 965]
---

The probe's axe pass (#965) reported `scrollable-region-focusable` on the
`fix-an-expression-with-a-quick-fix` walkthrough. The node is the expression field's diagnostic echo,
the source line with a caret under the fault: it scrolls sideways on a long line, holds only text, and
was `aria-hidden`, so a keyboard could not reach it. It is now a focusable, named region
(`tabindex="0"`, `role="region"`, `aria-label`), and the design system records the rule that a
scrollable region needs a focus stop. The echo still scrolls rather than wraps, because the caret
only stays under its column on one unwrapped line. The preview owner list and the Drawer body hold
links and form controls, so they already have a focus stop and are unchanged.
