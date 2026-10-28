---
title: "The console inbox and alarm subscriptions page (#1355)"
date: 2026-10-28
refs: [1341, 1355]
---

The console half of people as notification targets. A bell in the top bar counts the caller's
unread inbox entries (polled every 30s, as the alarm surfaces poll, and refreshed on every mark), and
**Inbox** lists them newest first with mark read, mark all read, an unread filter, and an empty state
that names what reaches it. **Profile, Alarm subscriptions** lists the caller's personal rules (pause,
edit, delete) and opens one in a drawer whose when is checked as it is typed and whose **Test** judges
the draft against the caller's own recent alarms through `POST /auth/me/action-rules:testDraft`, so a
first subscription is tried on real alarms before it is saved. The action expression field gained a
keyless mode for a site that judges no sample key.
