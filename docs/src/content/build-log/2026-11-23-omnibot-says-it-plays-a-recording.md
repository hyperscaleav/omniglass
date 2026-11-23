---
title: "Omnibot says when it plays a recording and answers only what was recorded (#2443)"
date: 2026-11-23
refs: [2443, 2121, 2298]
---

A lab with no live model plays an omnibot recording, and it played its default turns for any
question: asked "Which alarm is worst?", the panel read the meeting 405 video bar and proposed
writing a tag, with no text answer. A demo audience saw a wrong answer rather than a replay.

A recording can now name its prompts, and the lab's two do. The server refuses any other message
with a typed 422 ("This lab plays a recording; connect a model to ask your own question") before it
touches the conversation, and `GET /assistant/conversations` reports `live`, `replay` or `none`
with the recorded prompts and nothing of a model's configuration. The console shows a Recorded demo
note, the prompts as chips that play to their answer, the server's refusal with the chips again for a
question of its own, and marks the recording's write on its confirm card. The empty panel is one line
on what omnibot does, the chips and the page it will read. With a live model none of this appears.
Decision: ADR-2443. Regression tests: `TestOmnibotReplayRefusesAMessageItDoesNotAnswer`,
`TestReplayAnswersOnlyItsRecordedPrompts` and the panel's replay cases in `OmnibotPanel.test.tsx`.
