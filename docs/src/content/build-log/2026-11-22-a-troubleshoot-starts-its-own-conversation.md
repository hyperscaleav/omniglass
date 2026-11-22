---
title: "A troubleshoot starts its own conversation (#2286)"
date: 2026-11-22
refs: [2286]
---

An alarm's Troubleshoot button appended the triage to the conversation omnibot already had open, so the
triage ran on top of unrelated history and its tool results. A triage asked for from outside the panel
now starts a new conversation, as New conversation does, and shows only the triage. It waits while a
run is in flight, as before. Prompts typed in the composer still append to the current conversation.
