---
title: "Creating a location or a system ends on its Overview, with no failed request (#2465)"
date: 2026-11-23
refs: [2465, 1972]
---

Every location create asked `POST /locations:renderLabel` with no name as soon as a type was chosen. Every
shipped location type carries no name rule, so the route answered its documented 422 and the browser logged
it as a console error. The form now reads the type's `name_rule` from the list it already holds and asks
no nameless draft for a type that cannot mint a name; once a name is typed it asks with the name. The route
keeps its refusal.

Creating a location, system or component now lands on the new row's Overview with a "created" notice
instead of its editor (`?edit=1` stays the deep link for editing). The create page's back link names the
place it was opened from. The label field's hint says the label is rendered and keeps the raw template behind
the field's (i). The start-from-template form gained Cancel, and a system template list says how many of
the standards ship a system template. A system's map crops to the positions it holds, so a system with a
few members is a small map.
