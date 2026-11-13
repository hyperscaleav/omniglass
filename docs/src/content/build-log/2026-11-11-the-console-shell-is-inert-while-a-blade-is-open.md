---
title: "The console shell is inert while a blade is open (#25)"
date: 2026-11-11
refs: [25, 1203]
---

The blade stack trapped Tab and marked its top blade `aria-modal`, but the page behind it stayed
live: screen readers still walked the sidebar, the top bar and the list rows, and `aria-modal`
alone does not hide the background in every assistive technology. The shell root in `App.tsx`
now carries `data-app-shell`, and the stack sets `inert` on it while any blade is open and
removes it when the last one closes, in the same effect family as the focus restore. Blades, toasts
and the command palette portal to the body, outside that root, so they stay interactive; the Tab
trap stays as the fallback for a browser without `inert`. Tests in `BladeStack.test.tsx` hold the
attribute through push, drill, pop and close, the blade outside the inert root, and focus returning
to the opener.
