---
title: "admin revoke_session now records the target principal (#179)"
date: 2026-11-12
refs: [179, 172, 178, 527]
---

An admin's session revoke is now audited with both the acting admin (as before) and the target principal, answering "whose sessions did this admin revoke?" through the audit trail. The storage layer adds `WriteAuthEventFor(ctx, actorID, verb, resource, resourceID string)` for auth events that name a target, while self-service logout keeps `resource='auth'` with no target. The admin lane calls it with `resource='principal'` and the target id.
