---
title: "A blade heading tracks its row"
date: 2026-08-04
refs: [579]
form: bullet
---

- **A blade heading tracks its row** ([#579](https://github.com/hyperscaleav/omniglass/issues/579)).
  Renaming an entity from its blade updated the row, the list, and the blade body, and left the
  heading showing the old words until the blade was closed and reopened. The cause was one line of
  placement repeated eight times: `const r = row()` in the component body, where a Solid read
  subscribes to nothing because the body runs once. The lookup was correct; only the tracking was
  missing, which is why the [#572](https://github.com/hyperscaleav/omniglass/issues/572) guard, which
  checks that a heading resolves its row at all, passed the whole time.

  `BladeTitle` is now the heading, so the placement lives in one component rather than being retyped,
  and a second guard checks the narrower rule the first one could not see: a `*Title` component may
  not bind its row accessor to a const in its body. Found not by a test but by driving edit, Save,
  and read through a blade while verifying the [#574](https://github.com/hyperscaleav/omniglass/issues/574)
  rollup: both modes screenshotted clean and 834 tests were green, because nothing asserted a heading
  after a write.
