---
title: "Configure place lands on Configure, followed in the console (#932)"
date: 2026-10-20
refs: [932]
---

**Configure place** on a sole system's place card links to the place with `?tab=configure`, and the
place's view hands its address on to the system, query kept. Followed inside the console it landed
on the system's Overview: the redirect read the query off the window, which on a client-side
navigation still holds the page being left. It reads the router's own location now, the same
fix for the three name-to-uuid redirects (place, system, component) that read the window the same
way. A component test follows the link from another page, the path a direct load never took.
