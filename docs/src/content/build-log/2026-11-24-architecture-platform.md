---
title: "The Architecture section's platform pages: storage, identity and access, settings, scaling (#2320)"
date: 2026-11-24
refs: [2320, 2153]
---

Four Architecture pages are rewritten fresh from the code and badged `Partial`: Storage, Identity and
access, Settings, and Scaling and deployment. Identity and access now opens with the two
authorization invariants (a permission on every route, ABAC scope injected by the Storage Gateway on
every query) and the tests that hold them. Each page fences its unbuilt parts with an open issue: the
old fences that named the closed #430 now name #2611, and two new issues track an outpost's own bus
credential (#2616) and running more than one server (#2617). The pages keep their URLs and the
anchors other pages link to, and leave the Being rebuilt sidebar group.
