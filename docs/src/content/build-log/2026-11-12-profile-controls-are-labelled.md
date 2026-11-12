---
title: "Profile controls are labelled (#973)"
date: 2026-11-12
refs: [973]
---

The Username and Email inputs on the `/profile` page now have programmatic labels: each input carries an `id` and its `<label for=...>` points to it, matching the pattern already used for the Name field. The Email input's placeholder was removed, since a non-empty placeholder is not a label and disappears once a value is set.

As a result, `make probe` reports zero `label` violations on `/profile`; screen readers announce both fields by name.

The fix is tested by a new vitest that resolves both inputs via `getByLabelText("Username")` and `getByLabelText("Email")`, consistent with WCAG accessible name guidance.
