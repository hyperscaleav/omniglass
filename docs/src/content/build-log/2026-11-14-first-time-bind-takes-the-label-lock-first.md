---
title: "A first-time bind and a label cascade no longer deadlock (#1214)"
date: 2026-11-14
refs: [1214, 504]
---

A first-time bind (`PUT /systems/{name}/members/{component}`, or a role assignment to a component
that is not yet a member) inserted into `system_member`, whose foreign key shares the system's row,
and only then waited for the `label/recompute` lock. A cascade holding that lock waits for the same
row `FOR UPDATE`, so Postgres aborted one side with 40P01 and the caller saw a 500. Every membership
write now takes the label lock first, then the component's membership lock: bind, role assignment,
unbind, re-default and a system's release, so no two of them take the pair in opposite orders. This
is the first of the two fixes the issue offered; it adds no serialization, since each of those writes
already took the label lock for its restamp, only earlier. Storage tests hold the lock on a raw
transaction, start the bind, and play the cascade's row locks, 50 rounds per bind path, and assert
neither side deadlocks and the membership exists. The storage page states the lock order.
