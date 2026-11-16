---
title: "Platform alarms reach the admin role through a shipped rule and a role target"
date: 2026-11-16
status: Accepted
summary: "A notification target of kind role reaches every human holding the role, directly or through a group grant, read at each transition and scope-filtered per person. The boot seed ships a platform-admins role target and a platform-alarms-to-admins rule (when: platform, a new action-site binding), authoritative on their own rows except enabled; operator-added targets survive. The health page's email-not-configured line is deferred to #2016."
refs: [1987, 1341, 1623, 2016, 2017]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [alarms and actions](/architecture/alarms-actions/#role-targets-and-the-shipped-platform-routing), [identity and access](/architecture/identity-access/)
- **Decision:** (#1987, part of the self-alarming epic under #1623.)
  1. **A role is a target kind.** `role` joins `webhook`, `email`, `principal_group` and `principal`,
     with its own `role_id` column (ON DELETE RESTRICT) and an arm of the kind CHECK. It is expanded at
     the transition as a group is: every active, unarchived human holding the role through a direct
     grant or a grant to one of their groups. The match is on the role alone, whatever the grant's scope;
     the per-person read-scope check ([ADR-1341](#adr-1341-a-person-is-told-about-an-alarm-only-while-they-can-read-it-groups-expand-at-queue-time-scope-is-checked-there-and-again-at-delivery))
     still decides which alarms each holder hears about. Someone holding the role twice is told once. A
     queued email is judged again at send and fails once its recipient no longer holds the role. Binding
     one takes `principal_group:read:admin`, as a group does. Opt-out applies as it does to a group.
  2. **"A platform alarm" is a binding, not a fact.** The action site gains a derived `platform` boolean,
     true when the alarm's component is a node's or a server's platform component (a member of the
     reserved `platform` system), read in the same statement as `severity_rank`. The facts themselves are
     unchanged, so a template or a webhook envelope does not grow a field.
  3. **The seed owns its rows, the operator owns `enabled`.** The boot seed upserts the `platform-admins`
     target and the `platform-alarms-to-admins` rule in the `og` namespace: label, description, `on`,
     `when`, the role and the channels are put back on every boot, while `enabled` (and the rule's
     `opt_out_allowed`) is left alone, so a disable survives a restart. The rule's link to its shipped
     target is re-asserted and links an operator added are kept. A deleted shipped rule or target is
     seeded again at the next boot; `enabled` is the way to silence it. A shipped target's name taken by
     an operator target of another kind fails the boot rather than overwriting it.
  4. **Every transition now reads its facts.** Before, a fresh install had no platform action rule, so
     a transition skipped the facts read. With the shipped rule enabled it always runs, which moved the
     alarm write-cost pin by seven statements per transition (`internal/storage/alarm_cost_test.go`).
- **Divergence:** #1987's spec asks the platform health page to say email is not configured when no
  relay is set. That line is deferred to #2016; meanwhile, the inbox rows deliver and each email row fails
  with the reason. The console lists, shows and edits a role target, but its create form does not offer
  the kind yet (#2017).
