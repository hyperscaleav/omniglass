---
title: "The groups blade adopts the edit param"
date: 2026-08-14
refs: []
form: bullet
---

- **The groups blade adopts the edit param.** ADR-0120 left the groups blade on its one-shot
  signal "until it gains an id deep link"; it already had one (`?g=<id>`), so the adoption was
  the Users port verbatim: `?g=<id>&edit=1` opens the group's blade already editing behind the
  manage gate, the create flow hands off through that URL, and `openGroupInEdit` /
  `consumePendingGroupEdit` retire. No console one-shot edit signal remains (#762).
