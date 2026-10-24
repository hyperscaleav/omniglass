---
title: "A detail view is one per system, its place a card of context (#872)"
date: 2026-10-10
refs: [872]
---

The outline left the workspaces "reviewed separately", and the review found them still speaking
what the outline retired: a location page that was a band canvas with inert dashed holes and a
cards/list toggle, a room and its system as two views under two names ("Meeting Room" titled
under a "Briefing Room" crumb), three frames for one idea (the tabs above the counts line on two
kinds and inside the card on the third), facts said twice (slots in the counts line and the
header, metrics as tiles and as a table, the history on two tabs), and a side panel that was a
configuration form. The architect's ruling: the model is right, the orientation was not.
Systems are the unit, places are folders and metadata, components are pieces of systems, and
people care about their systems (ADR-0139).

So a system's view is its place as a card of context above the system card and its tabs, and a
room holding one system lands on it. A folder place (none, or several) keeps a view of its own:
a brief card per system, then the outline rooted there, which needed `OutlineWorkspace` lifted
out of Explore and `rootAt` added to the outline model. A system's components are rows in the
outline's idiom, a role grouped only where it says something, the KPI tiles fold into Data, and
the history is Activity's alone. The side panel is the glance (verdict, why with severity, the
place or the systems it serves with the role in each), the form's configuration panels staying
on Configure. The shipped system label rule leads with the place, so a fresh fleet's rooms and
systems read as one name, and healthy is grey on every pill, strip and marker. The band
builder, `SystemCard`, `FleetRows`, `FleetShell` and `fleet_tiles` are deleted and guarded.

The live console found two things the component suites could not. A room holding two systems
re-mounted its system cards hundreds of times a second: the list iterated the system objects, a
fresh fleet read handed back new ones, and each rebuilt card's reads refetched the view (the cards
now key on ids, behind a test proven red on the old code). And a role whose one device was down
drew an "empty slot" beside that device, because the gap was counted from the shortfall rather
than from unstaffed seats.

One claim did not survive the audit: the component page said a component's place "follows the
primary system", and the schema carries no such inheritance (a component's place is only what
was set). The view now says which it is showing, the component's own place or its system's
standing in, and the storage question stays with #862.

The label rule's first cut read the place alone, so two systems of different kinds in one room
(a classroom and its signage) both read "Media Lab". The architect's ruling: a shared place names
each system's kind ("Media Lab Meeting Room", "Media Lab Signage"). That is a fact the rule could
not see, so the system's data map gained `SharesLocation`, and a create, a move or a delete restamps
the systems at the places whose count it changed, in the same transaction.
