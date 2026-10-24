---
title: "The rules page, the severity levels beside it, and the epic's docs (#819, slice #921)"
date: 2026-10-19
refs: [819, 921]
---

`/rules` went from a stub to the event rule registry: each rule's key, its criteria in words, its
severity in the level's color and its live state, read from `GET /event-rules/{name}/state` (the alarms
it holds open and its streaks waiting out a sustain), with a blade listing both linked to their
components and create, edit, disable and delete in a drawer, validated in TypeScript; official rules are
read-only. **Catalog > Actions > Severity** sits
beside it, a read view of the levels worst first with rank, color and effect (authoring stays CLI and
API, #930), and every alarm chip in the console renders through the registry. "Last evaluated" did not
ship (#929, ADR-0144 point 7). Writing the docs caught the console teaching the wrong thing: the
disable hint and the delete confirmation said a rule's open alarms stay open, where the server clears
them; both now say so, under a test. The seed facts render the shipped levels and the official rule, so
the alarms page and the new [rules guide](/guides/admin/rules/) no longer restate them by hand. The
closed loop was walked on a simulated fleet with NET_ADMIN: unplugging a component opened the official
rule's High alarm and degraded its system, replugging cleared both, and a rule created on `/rules` opened
its alarm, in an operator level's label and color, when a simulated panel's input moved.

Review hardening on the same slice. A rule edit is now judged on what it changes from the stored rule
(a restated selector entry, key or criteria is not re-resolved), and the console PATCH sends only the
fields an edit changed, so a rule watching a since-deleted component or type stays editable; the form
marks such an entry **deleted**. The list and get carry `open_alarms` and `pending_streaks`, read for the
whole list in one aggregate query, so the page polls the list every 10s instead of each row's `/state`.
`GET /auth/me` gained `fleet_permissions` (the permissions on all-scope grants), and every rule write in
the console is gated on it, so a caller holding `rule:create` only on a place is no longer offered one.
Criteria values are kept exact (edge spaces refused, a metric a strict decimal). Rules now read like a registry
(any `rule:read` lists them, with counts, open alarms and streaks fenced to the caller's component read
scope), while writes keep an all-scope grant (ADR-0144, amended).

The slice carries the epic's walkthrough, Watch a rule raise an alarm, embedded in the
[rules guide](/guides/admin/rules/): a simulated touch panel unplugged from its page, the official
rule's blade showing the streak wait out its 15s sustain and then the open High alarm, and the panel's
system degraded with its Touch Control role short. It waits on the blade's own poll rather than holding
for a fixed time, and it stops at the open alarm: the probe's detection, the sustain and the sweep tick
take about 31s of the 60s budget, which leaves no room for the replug and the clear.
