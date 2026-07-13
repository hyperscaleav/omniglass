---
title: The audit trail
description: "The read-only record of every privileged action and every sign-in, including who acted behind an impersonation."
---

**Settings > Audit** (with `audit:read`, so **administrators and owners** only) is the read-only audit trail:
every privileged action and every sign-in, newest first, each with when it happened, who did it, the action,
and the resource. An action taken while impersonating shows the **real administrator** as the actor, with an
`as <account>` tag naming the principal whose identity they assumed (for example `admin as bob`), so
accountability lands on the human who acted and impersonation never hides them. A read-only user (a viewer) does not
see this page: the audit trail is admin-level information, so a plain "read everything" grant does not open it.
Failed sign-ins on a real account show as **login failed** (and a sign-in to a disabled account as **login
denied**), so you can spot a brute-force attempt; attempts on usernames that do not exist are not recorded.

The page uses the same faceted search as the inventory lists: filter by **who**, **action**, **resource**, or
**id** (type a term for a quick actor search, or `action:login` to pin a facet), and combine chips to narrow.
Filtering runs over the rows already loaded; **Load older** pages further back in time, so a search that comes
up short is a cue to load older and look deeper.

The model behind the trail, what is recorded and the dual-actor rule for impersonation, is
[identity and access](/architecture/identity-access/) and [audit](/architecture/audit/).
