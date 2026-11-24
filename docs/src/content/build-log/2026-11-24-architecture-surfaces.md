---
title: "The Architecture surfaces are rebuilt: API, UI, views and AI (#2320)"
date: 2026-11-24
refs: [2320, 2153]
---

Four Architecture pages are rewritten fresh from the code and badged `Partial`: API, UI, Views and AI.
The API page is the conventions every route follows and links the generated reference rather than
restating routes; the per-resource detail it used to carry lives on the subsystem pages and in the
reference. Views and AI leave the Design sketches list, since the view binding and omnibot are built;
named views, the agent's fleet query tool, the dashboard canvas and the assistive-to-operational AI
spectrum stay fenced with their issues. The UI page drops the three design blocks that still tracked
the closed #523.

The frozen architecture glossary is deleted: the generated [glossary](/reference/glossary/) is the
one list of terms, `/architecture/glossary/` redirects to it, and its inbound links are repointed.
