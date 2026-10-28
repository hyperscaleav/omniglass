---
title: "A person is told about an alarm only while they can read it: groups expand at queue time, scope is checked there and again at delivery"
date: 2026-10-28
status: Accepted
summary: "A group or person target expands to its human, active members inside the alarm transition (one `notification` row per person per channel, each with its own delivery state and Idempotency-Key), and a person is kept only when their own component read scope admits the alarm's component, judged set-based under #618's savepoint so it can never fail the transition. The scope is checked again where a row waits: an email row at send, an inbox entry at every read. A personal rule is gated by its owner's scope before its `when` runs. Inbox entries are `notification` rows (`channel = inbox`); the recipient's name is the recipient's and `principal:read:admin`'s alone"
refs: [1341, 1352, 1317, 1353, 1354, 618]
---

- **Date:** 2026-10-28 | **Status:** Accepted | **Pages:** [alarms and actions](/architecture/alarms-actions/), [identity and access](/architecture/identity-access/), [workers](/architecture/workers/)
- **Decision:** People are notification targets without a parallel model (#1341, its open decisions approved as recommended):
  1. **Expand at queue time.** A `principal_group` or `principal` target is expanded to the people it
     names inside the alarm transition's transaction, reading the membership as it stands, so a member
     added after the rule was written is told on the next transition and a removed one is not, with no
     rule edited. Only active, unarchived humans are recipients (a service, node or agent member is
     skipped). Each person gets one row per channel, so each has their own delivery state, retry and
     `Idempotency-Key`; a person one rule reaches twice (two of its groups, or a group and directly) is
     told once per channel.
  2. **Check scope at queue time, set-based, under the savepoint.** A person is kept only when their own
     component read scope, resolved from their effective grants exactly as a request of theirs would be,
     admits the alarm's component. The check is one statement for the members, one for all their grants,
     one for the role index and one ancestor walk of the component, then a pure resolve per person, so
     its cost does not grow with a group; it runs under #618's savepoint, so a failure in it writes no
     person row and the transition commits.
  3. **Check again where a row waits.** An inbox entry is delivered by its own commit, and its read is
     itself scoped: the inbox reads the caller's rows filtered by their component read scope now, so an
     entry about a component they lost is gone. An email row (#1317) re-checks its recipient at send
     (still exists and active, still reads the component, still a member of the group it came through)
     and fails with a plain reason instead of sending.
  4. **A personal rule never matches outside its owner's scope** (#1353): the owner's component read
     scope gates the alarm before the rule's `when` is evaluated, so neither a delivery nor the `when`'s
     result can reveal an alarm the owner could not read.
  5. **Inbox entries are `notification` rows**, `channel = inbox`, written `sent`: one history per alarm,
     the timeline and `/notifications` already show them, and the component scope filter is already
     injected on `component_id`.
  6. **A recipient's name is the recipient's and the directory admin's.** A person-addressed row is
     visible wherever its alarm is, but its recipient is named only to that person and to holders of
     `principal:read:admin`, since the principal directory is admin-tier; everyone else sees it withheld.
- **Context:** The alternative was one row per group, fanned out by the delivery tick. It would carry a
  rendered message about an alarm to a list nobody had checked, keep membership out of the alarm's
  history, and share one delivery state among many people. Queue time is the one point where the
  rendered message and the person are known together. The second check exists because a row can wait:
  an email retries for up to about 21 minutes, and an inbox entry waits until it is read, in which time
  a grant can be revoked. A send-time-only check would cover email but not the inbox, whose delivery is
  its commit.
