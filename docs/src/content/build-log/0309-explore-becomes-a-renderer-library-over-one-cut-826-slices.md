---
title: "Explore becomes a renderer library over one cut (#826, slices #837 to #841)"
date: 2026-10-04
refs: [826, 837, 841]
---

The Miller-column drill that slice 2 built was replaced before it shipped. Tested against a
generated fleet of a thousand systems across twelve roots of four different tree shapes, it
failed three ways: a screen spent on four levels of tree, a uniform depth assumed that
`location_type` and `allowed_parent_types` do not guarantee, and one question answered where
operators arrive with several.

What replaced it is four renderers over one model. `place_cut.ts` chooses, per root, the
shallowest container type that root has at least two of, so a campus of buildings and a
two-level annex sit side by side with each card naming its own type; the rule took three
attempts, and the two wrong ones (excluding childless types, breaking ties on a global tier)
each passed their unit tests and were caught by a render and by the e2e walk. `view_budgets.ts`
turns labels, area and z-order into things a view can afford rather than settings an operator
chooses: the label ceiling is twenty-four rooms, measured against the console rather than
picked. `explore_view.ts` is the one model every renderer consumes, so cards, bands, mosaic and
matrix cannot disagree about which card a system lands in or what a count says.

Two rules were earned by looking at output. A mosaic tile's fill is the **share** needing
attention, not a worst-wins rollup, because the first render came out uniformly red: at any
realistic failure rate almost every aggregate contains one outage. And the mosaic's layout is
integer pixels with edges snapped once and widths derived from them, proved as a property test
that every pixel of a frame is covered exactly once, because the percentage version left seams
and overlaps that read as data.

`presets.ts` saves a way of looking under the name of the job it serves. A preset is a snapshot
of the same object the controls write to, so it can never mean something the controls cannot
produce, and it carries no scope at all: that omission is the line between this page and a
dashboard, and there is a test asserting it.

The page then took the console's standard chrome, which is what the rest of the fleet already
wears: a counts line on top whose need-attention count is itself the quick filter, then
`ListShell` (filter bar and card) around the body. Three bespoke things retire with it. The
search box and its hit list become the filter bar's bare term, matching a system by name or by
the place it sits in, and `lib/explore.ts` is deleted with them. The attention checkbox becomes
the counts line's own button, writing the same verdict chip the filter bar shows, so the two
cannot disagree. And the status line folds into the counts line. Filtering drops a card whose
systems all fall outside it, which is what makes the filter read as a search; the filter keys
are verdict, location type, standard, path and name, and deliberately not a location facet,
since naming a subject is the drill's job and the line this page holds.

Two defects came out of the alignment. The page's `attentionOf` had excluded `incomplete` while
the console's fleet tiles had always counted it, so the counts line said one when two systems
needed somebody; the older surface won, and the badge and the mosaic then had to learn that
incomplete is its own hue rather than a shade of degraded or the colour of an empty tile. And
the hover readout grew the counts line when a dot was hovered, reflowing the page under the
pointer and moving the dot out from under the click that was landing on it, which the e2e walk
caught; its slot is now reserved.

A readiness pass over the whole surface, driven against the console state by state, then found
what the unit suites could not. Every `xs` button in the console was drawing at 40px, larger
than an `sm` one: the `Button` primitive assembled its size class at runtime, the stylesheet
scan never saw `btn-xs`, and the rule was never emitted, so the preset bar and the counts
line's filter stood taller than the cards they framed. The class names are spelled out now and
a test pins that every class the primitive emits is one the scan can see. The incomplete dot
was the neutral, which on this theme is the card's own ground, so the one dot asking for
somebody was invisible; it wears the commissioning hue its badge wears, and a mosaic tile's
name takes an ink chosen from its fill, since one ink for every tile vanished on the empty one.

The counts line now counts where the operator is standing. Beside the breadcrumb, "Headquarters,
41 systems" read as a claim about Headquarters, and the fleet's attention count offered inside
a node holding none filtered that node to nothing. A section header under a filter says how
many of the place's cards are drawn ("1 of 4 rooms") instead of passing a filtered count off as
a fact about the place, and it pluralises a customer's type name as the word goes. A control is
offered only under a renderer it changes, the matrix follows the drill its own rows trigger,
`standard:` filters under every renderer rather than only after the matrix had loaded the join,
and a drilled header opens the location's own workspace, which the table face had been the only
road to. The face toggle and the kind tabs became the controls the workspaces already wear. A
link's `?chips=` and the browser's stored view are checked on the way in, so a hand-edited link
or a renderer key from an older build draws the fleet instead of nothing.

The last of the inventory-era surface went with it. The Locations table still carried a KPI
chip row (a summary board with a type-mix donut) that ADR-0137 had retired everywhere else, so
the board, its `TreeList` plumbing and `Donut` are deleted; so are the health and members
panels the classic faces had left unmounted since #806, the name-address fallback no fleet
table can reach now that an identity route is a workspace, and the tile and band helpers the
canvas alone had read. The retired-surfaces guard names each one.
