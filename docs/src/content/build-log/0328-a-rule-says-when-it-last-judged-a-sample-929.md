---
title: "A rule says when it last judged a sample (#929)"
date: 2026-10-19
refs: [929]
---

The rules page could not tell a quiet rule from one whose key had stopped arriving. Each rule now
carries `last_judged_at` on the list, a get and the state route, shown as the Live column's tooltip and
the blade's **Last judged** fact. The #920 review had kept a per-sample write off the ingest path, so the
stamp is coalesced (ADR-0144 point 7, amended): an ingest batch stamps the rules it judged once, as its
last statement, only when a rule's stamp is at least 5 seconds old, skipping a stamp row another batch
holds, in a new `event_rule_live` table rather than on the rule row every batch holds. Testcontainer
tests prove the stamp moves only on a judgment (not a disabled rule, an unwatched component or a
history sample), coalesces inside the bound, agrees across the list, get and state, and never makes a
batch wait.
