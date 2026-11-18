---
title: "The audit list scrolls inside its card (#1461)"
date: 2026-11-16
refs: [1461, 1955, 2003]
---

The audit trail held its space with 12 skeleton rows (#1955), but a first page holds 100, so the
loaded list still pushed its footer (the count and **Load older**) off the bottom of the screen: the
probe scored `route:/audit` at 0.102 against a 0.1 budget once the lab banner's shift was gone (#2003).
The page now turns on the list's fill mode inside a box sized to the viewport, so the rows scroll
under sticky column headers and the footer stays where the first paint put it. Fill mode needs that
box: the console scrolls the document, so a list with no definite height around it grows with its
rows. `route:/audit` now reads 0.002 to 0.004 over three runs, with no footer among the shift sources.
