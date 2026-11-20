---
title: "Omnibot keeps a model's reasoning apart from its answer (#2259)"
date: 2026-11-20
refs: [2259]
---

Bedrock's gpt-oss inlines its chain of thought in `content` as `<reasoning>...</reasoning>`, which
reached the panel as part of the answer and went back to the model on every later turn. The
OpenAI adapter now splits leading `<reasoning>` blocks off the content, and reads the
`reasoning_content` and `reasoning` fields other servers use, into `Turn.Reasoning`. The loop
keeps it on the stored message and reports it as an `EventReasoning`; the run narrates it as
`REASONING_MESSAGE_START/CONTENT/END`; the request builder never sends it back. The panel shows
it under a collapsed "Thinking" disclosure, apart from the answer.
