---
title: "Every gated resource registers its scope (#754)"
date: 2026-11-12
refs: [754, 749, 1206]
---

A permission whose resource the scope resolver does not know now fails `make test`, naming the route
and the permission. The resolver used to answer an unregistered resource with the empty set for every
grant but an `all` one, so a route fenced on it would have denied every scoped caller while an owner
kept working, which is how `command:issue` nearly shipped in #749. The new test reads the permission
stamps off every operation in the generated OpenAPI, so it tracks the routes with no list to keep; the
catalog and directory resources that only an `all` grant confers are now declared fleet-wide rather
than left to the default. Behind it, a resolved scope remembers the resource it was resolved for, and
the gateway's scoped and placement resolves refuse a set whose resource cannot be checked against the
table in front of them with a logged wiring error (a 500), instead of a denial that reads like an empty
grant.
