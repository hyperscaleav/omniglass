---
title: "A member opts out of a group notification the admin allows (#1354)"
date: 2026-10-28
refs: [1341, 1354]
---

A platform action rule gains `opt_out_allowed` (default false) and `action_rule_opt_out`. A member of
a group the rule targets opts out for themselves with `POST /action-rules/{name}:optOut` and back in
with `:optIn` (`notification:subscribe`, audited); a rule that does not allow it answers 409. The
opt-outs of the firing rules are read in one statement and skipped when their group targets expand,
a pending email from that rule fails at send for a member who opted out after it was queued, and
taking `opt_out_allowed` back off restores every member from the next transition. A target that names
a person directly is not affected by their opt-out.
