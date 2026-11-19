---
title: "Platform alarms reach the admin role through a shipped rule and a role target"
date: 2026-11-18
status: Accepted
summary: "A notification target of kind role reaches every person holding the role, directly or through a user group's grant, read from the grant table at each transition and scope-filtered per person. The boot seed ships two inbox-only role targets (platform-admins, platform-owners, since owner does not inherit admin) and a platform-alarms-to-admins rule (when: platform, a new action-site binding read before any rule). All three are official: what the seed ships is refused to an edit or a delete, while enabled, a target's label and operator-added targets are the operator's. Email and the health page's relay line wait on #2016."
refs: [1987, 1341, 1623, 2016, 2017]
---

- **Date:** 2026-11-18 | **Status:** Accepted | **Pages:** [alarms and actions](/architecture/alarms-actions/#role-targets-and-the-shipped-platform-routing), [identity and access](/architecture/identity-access/)
- **Decision:** (#1987, part of the self-alarming epic under #1623.)
  1. **A role is a target kind.** `role` joins `webhook`, `email`, `user_group` and `user`, with its own
     `role_id` column (ON DELETE RESTRICT) and an arm of the kind CHECK. It is expanded at the transition
     from the grant table, as a group is from its membership: every active, unarchived person holding the
     role through a direct grant or a grant to one of their user groups. The match is on the role alone,
     whatever the grant's scope; the per-person read-scope check ([ADR-1341](#adr-1341-a-person-is-told-about-an-alarm-only-while-they-can-read-it-groups-expand-at-queue-time-scope-is-checked-there-and-again-at-delivery))
     still decides which alarms each holder hears about. Someone holding the role twice, or reached by two
     targets on one rule, is told once. A queued email is judged again at send and fails once its recipient
     no longer holds the role. Opt-out applies as it does to a group.
  2. **Binding a role reads the role directory.** Creating a role target takes `role:read:admin` and
     `user:read:admin` (it names a role, and through it a set of people), and the role a target names is
     shown only to a caller holding `role:read:admin`; anyone else reads the target's kind alone.
  3. **"A platform alarm" is a binding, not a fact.** The action site gains a derived `platform` boolean,
     true when the alarm's component is an outpost's or a server's platform component (a member of the
     reserved `platform` system). It is read before any rule runs, so a transition on a fleet component
     drops a rule whose `when` is `platform` alone without reading the facts: the shipped rule costs a
     fleet transition one statement (`internal/storage/alarm_cost_test.go`). The facts themselves are
     unchanged, so a template or a webhook envelope does not grow a field.
  4. **Two targets, because owner does not inherit admin.** The bootstrap user holds `owner`, so a target
     naming `admin` alone would tell nobody on a fresh install. The seed ships `platform-admins` (role
     `admin`) and `platform-owners` (role `owner`), both on the rule; a person holding both is told once.
  5. **The shipped rows are official.** A target gains an `official` flag as a rule has one. The boot seed
     upserts the two targets and the rule in the `og` namespace and is authoritative on what it ships: a
     target's role and channels, a rule's label, description, `on`, `when` and its links to the shipped
     targets. The API refuses an edit of those, or a delete, with a 409 (`ErrNotificationTargetOfficial`,
     `ErrActionRuleOfficial`). What the operator owns survives a restart: `enabled` on each row, a target's
     label, the rule's `opt_out_allowed`, and targets an operator adds to the rule. Carrying `official`
     makes `notification_target` a namespaced catalog ([ADR-0318](/architecture/decisions/#adr-0318-every-catalog-key-is-a-namespace-and-a-key)):
     a name is unique per namespace and a bare name resolves along the search path, so an operator's
     `local` target of a shipped name shadows it rather than failing the boot, and the seed links the
     shipped rule to the `og` targets, never to the operator's.
  6. **Inbox only, for now.** The shipped targets carry `inbox` alone: with no `mail_relay` set, an email
     channel would fail a row per person per transition on every install. Email joins once such a send is
     skipped rather than failed (#2016); an operator wanting mail adds an email-channel target of their own.
- **Divergence:** #1987's spec asks for inbox and email, and for the platform health page to say email is
  not configured when no relay is set. Both wait on #2016. The console lists, shows and edits a role target
  (its role and, on a shipped one, its channels read-only), and offers the shipped rule's enabled switch,
  but its create form does not offer the kind yet (#2017).
