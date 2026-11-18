---
title: "Omnibot in the console, a panel that shows its work (#1167)"
date: 2026-11-17
refs: [1167, 2121, 2134, 2135, 2111]
---

The console opens omnibot in a panel from the top bar's **Ask omnibot** button and the command
palette's last row, which carries the typed query as a draft (ADR-1167). The panel runs each prompt
over the server's `:run` stream (ADR-2121) on one conversation, and shows omnibot's text and a card
per tool call. A per-tool renderer registry shows a component read as a link and reachability as the
endpoint ladder; any other tool, or a result its renderer cannot read, falls back to a generic card,
with the raw result one click away. A call the agent policy asks about pauses on a confirm card whose
answer resumes the run; the step limit, a refused run and a failed one each read as what they are.
**Add a credential** creates the secret through the secrets route with the person's token and puts
only its name in the draft. Showing a driver spec and first samples is #2134, reopening an earlier
conversation #2135, and the over-budget state rides #2111.
