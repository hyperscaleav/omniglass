---
title: "A group or a person is told in the console inbox, only what each can read (#1352)"
date: 2026-10-28
refs: [1341, 1352]
---

The first slice of people as notification targets (#1341). A `notification_target` can now name
people: a `principal_group` target reaches every human member of a group and a `principal` target
one person, each on the channels it names (`inbox` here; email follows in #1317). They ride an action
rule's targets exactly as a webhook does.

The group is expanded inside the alarm transition, reading the membership as it stands, so a member
added after the rule was written is told on the next alarm and a removed one is not. Each member is
kept only when their own component read scope, resolved from their grants as a request of theirs
would be, admits the alarm's component. The filter is set-based (a fixed four statements and a pure
resolve per person; a test pins that a group of 2 and a group of 20 cost the same) and runs under
#618's savepoint, so a failure in it never fails the transition (a test hides the role table and the
raise still commits). An inbox row is written `sent`, delivered by its commit, and never drained by
the tick.

`GET /auth/me/notifications`, `:markRead` and `:markAllRead` read and mark the caller's own entries,
each read scoped again by the caller's component read scope as it stands, so an entry about a
component they lost is gone. `/notifications` and the alarm timeline carry each row's `channel` and its
recipient, named only to the recipient and to `principal:read:admin` holders. Binding a group or a
person takes the directory's admin-tier read, and a bound group or person cannot be deleted: the
refusal names the target.
[ADR-1341](/architecture/decisions/#adr-1341-a-person-is-told-about-an-alarm-only-while-they-can-read-it-groups-expand-at-queue-time-scope-is-checked-there-and-again-at-delivery)
records the queue-time expansion and the two-point scope check.
