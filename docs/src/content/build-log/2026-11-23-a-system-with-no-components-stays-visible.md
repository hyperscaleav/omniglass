---
title: "A system with no components stays visible in Explore (#2464)"
date: 2026-11-23
refs: [2464]
---

Delete a system's last component and its place read "Empty" in Explore, as if the system were gone, while
the system still answered at its address. The server was never the cause: the fleet view lists every
in-scope system with an empty dots list (pinned now by an integration test against Postgres). The outline
counted only components, so a place wearing a component-less system had nothing to say. It now reads
"no components yet" (and a system among several heads its own group with the same words); "Empty" is kept
for a place with no system at all.

A blank system's page and drawer (one shared body) replace the bare empty table with an empty state that
says what a system and a slot are, with Add component (the create form, `?system=` prefilling the new
component's system) and Start from a standard (the Configure tab). The guided flow is #2460.
