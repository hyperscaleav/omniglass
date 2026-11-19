---
title: "A system type names what a system is for (#2067)"
date: 2026-11-17
refs: [2067, 2034]
---

Type has one meaning (ADR-2034): the registry row that says what kind of thing an instance is. The
system type tree now says what a system is for, never the space it sits in. The seeded `room` node,
a space, becomes `collaboration` (same id, through a one-time migration so the boot seed updates it
in place), and its children read as uses: Board Meeting, Teaching, Meeting, Training, Conference,
Huddle. The ids (`board`, `class`, `meeting`, `huddle`, ...), stems and abbreviations stay, so no
generated name moves; a label the shipped rule renders from the type now reads, for example, Media
Lab Teaching 2. `internal/storage/system_type_use_migration_test.go` stands a fleet on the old row,
a system typed by it and an operator child under it, migrates and reseeds, and checks all three land
on `collaboration` with nothing left behind. Genus, the second word for a type, retires from every
operator surface and joins the docslint denylist; ADR titles and their anchors keep it.
