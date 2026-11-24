---
title: "The list-components step card is bounded and says where each row is (#2261)"
date: 2026-11-23
refs: [2261]
---

The omnibot step card for `list-components` drew every row it was given, by label alone, so a fleet
with 37 "Touch Panel 1" rows showed 37 identical links. The card now draws the first 10 rows behind
a "Show all N" toggle, and each row carries its place (location and primary system) from the
result, which already includes both fields, so no API or generated-client change. Regression tests:
`caps a long list behind a Show all control` and `tells same-labelled components apart by where
they are` in `web/src/components/OmnibotStep.test.tsx`.
