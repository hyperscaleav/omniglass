---
title: "The Name column stops being the first thing a narrow screen drops"
date: 2026-08-13
refs: []
form: bullet
---

- **The Name column stops being the first thing a narrow screen drops.** A list table is
  `table-layout: fixed` and every column but Name declares a width, so Name takes what is left. That
  is what lets the identifier grow into a wide screen, and it also made Name the first column to give
  space up on a narrow one, to the point of vanishing: measured against the dev estate at a 1280
  viewport, where the list card offers 973px, the Components table's Name column was **0px** wide
  while Tags kept all 340 of its pixels, and Systems was the same at 0px against 960px of declared
  columns. Locations, declaring 650px, had 173px left over and looked fine, which is the whole of why
  one page was reported and three were affected: identical markup, three outcomes, decided by the
  widths each page happens to declare. The remedy is one floor in the shared shell rather than
  numbers tuned per page: the table asks for a `min-width` of everything declared plus a Name
  minimum, so the browser gives Name that much and the card (already `overflow-x-auto`) scrolls
  sideways when even that does not fit. Wide screens are untouched, since `width: 100%` beats a
  smaller min-width and Name still absorbs the surplus. Held by a page test on each of the three
  pages (Name declares no width, and the table's floor leaves it the minimum) and by a browser test
  that MEASURES the rendered column at 1280 and 1366, which is the tier the defect lives in: a
  column measuring zero pixels sat on main behind a green suite because nothing below a real browser
  does layout.
