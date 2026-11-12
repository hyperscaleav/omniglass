---
title: "Breadcrumb crumbs are links (#875)"
date: 2026-11-12
refs: [875, 633]
---

Each breadcrumb crumb with a destination is a real link now, the router's `A` with an `href`, not a
`<button>` that called `navigate`. It can be middle-clicked or opened in a new tab, its address copied, a
screen reader announces a link inside the navigation landmark, and the target is in the DOM. The `Crumb`
type takes an `href` where it took an `onClick`; the component, location, system and component pages and
the subscriptions page pass the address they already computed. The current crumb stays plain text with
`aria-current="page"`, and the truncation rule is unchanged.
