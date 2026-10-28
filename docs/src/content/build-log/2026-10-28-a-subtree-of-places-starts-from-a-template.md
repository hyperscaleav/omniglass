---
title: "A subtree of places starts from a template (#1328)"
date: 2026-10-28
refs: [1328, 827]
---

`POST /locations:instantiate` (`omniglass location instantiate`) creates a subtree of places from a
place template under a parent or as a new top, with the systems it carries staffed and unattached, in one
transaction. Each place's type must be one the fleet holds and allow its parent's type, read from the
fleet's effective `allowed_parent_types`, so an operator who forked a shipped type is checked against
their fork; a type the fleet lacks or a placement its types refuse is a 422 naming the type or the pair.
Places are named by the template, the request (the root) or their type's name rule. No logic is keyed
on any particular type.
