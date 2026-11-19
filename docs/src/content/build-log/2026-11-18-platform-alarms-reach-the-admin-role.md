---
title: "Platform alarms reach the admin role by default (#1987)"
date: 2026-11-18
refs: [1987, 2016, 2017]
---

A platform alarm used to open on the console and tell nobody until an operator wrote an action rule for
it. A notification target can now name a **role**: everyone holding it, directly or through a user group's
grant, read from the grant table at each transition and filtered by each person's own scope, told once
however many ways they hold it. Binding one takes the role and user directory reads. The boot seed ships
two inbox-only role targets, `platform-admins` and `platform-owners` (owner does not inherit admin, and the
bootstrap user is an owner), and `platform-alarms-to-admins` (`when: platform`, a new action-site binding
read before any rule, so a fleet transition skips the rule without reading its facts). All three are
official: the seed puts back what it ships and the API refuses an edit of it or a delete, while each row's
enabled switch, a target's label and targets an operator adds stay the operator's across a restart.
The shipped targets live in `og`, which makes `notification_target` a namespaced catalog: an operator's
target of a shipped name shadows it, and the shipped rule keeps the shipped one. The
console hides a shipped target's delete, shows its channels read-only, and offers the shipped rule's
Disable with its Edit greyed. Integration tests grant and revoke `admin` around two platform alarms, reach
a person holding the role through a group and through both roles once, check a scope-limited holder hears
nothing outside their scope, boot the seed twice around a disable, and tell the bootstrap owner about a
server alarm over HTTP. Email on the shipped targets and the health page's relay line are #2016; the
console create form's role picker is #2017 ([ADR-1987](/architecture/decisions/)).
