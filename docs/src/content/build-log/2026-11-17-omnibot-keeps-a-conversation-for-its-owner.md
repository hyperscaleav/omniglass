---
title: "omnibot keeps a conversation for its owner, and --resume carries it on (#2113)"
date: 2026-11-17
refs: [2113]
---

A migration adds `assistant_conversation` and `assistant_message`, and the Storage Gateway reads and
writes them with the caller as the owner predicate on every query, so another person, the platform
owner included, finds nothing (`internal/storage/assistant_test.go`). Five routes under
`/assistant/conversations` serve them behind `assistant:use`, which `viewer` now carries; an append
names the last message it saw or is a 409, and every write is audited without content
(`internal/api/assistant_e2e_test.go`). The retention pass prunes a conversation
`assistant.conversation_days` (30) after it last moved. On the CLI, `omniglass omnibot --resume <name>`
carries a conversation on, and when its last run paused on a write it asks that call first: the stored
pending is only a marker, and the proposal is rebuilt from the transcript's open call and the live
tools, so a marker naming no open call is refused (`internal/cli/omnibot_resume_test.go`). ADR-2113
records why a kept transcript is not a server-side pending approval.
