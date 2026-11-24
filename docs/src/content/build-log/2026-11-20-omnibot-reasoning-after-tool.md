---
title: "Omnibot splits reasoning wherever it appears in a reply (#2303)"
date: 2026-11-20
refs: [2303]
---

After a tool call, Bedrock's gpt-oss could put its `<reasoning>` block after the answer text, so
the reasoning showed inline. The OpenAI adapter now takes every `<reasoning>` block out of the
content, wherever it sits, and keeps the rest as the answer. The raw post-tool Bedrock reply was
not captured here (no Bedrock access in this run), and `TestOpenAILive` was not run against it, so
that check is still open.
