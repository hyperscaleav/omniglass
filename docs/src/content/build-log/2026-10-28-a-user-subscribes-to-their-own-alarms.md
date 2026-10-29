---
title: "A user subscribes themselves to the alarms they can see (#1353)"
date: 2026-10-28
refs: [1341, 1353]
---

A **personal action rule** is an `action_rule` with an owner and the channels it tells its owner on
(inbox, email), and no targets. `/auth/me/action-rules` creates, lists, edits, pauses, deletes and
`:test`s the caller's own, gated by a new `notification:subscribe` that `viewer` carries; another
user's rule is a 404 on every verb, the platform `/action-rules` never lists one, names are unique per
owner, and purging the owner takes their rules.

At the transition every personal rule's owner is judged in one set-based pass, and a rule whose owner
cannot read the alarm's component is skipped before its `when` runs (ADR-1341, decision 4). The test
that proves it gives a component-scoped owner a rule whose `when` divides by zero, so running it
would write a failed row about the alarm; an alarm outside the owner's scope writes nothing at all,
and the test failed with the gate removed. A personal rule's email rows are judged again at send like
any person's, and its rule name is withheld from others with the recipient. `:test` judges the rule,
or a draft, against the owner's recent alarms in scope and writes nothing.
