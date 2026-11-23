---
title: "A recorded omnibot answers only the prompts it was recorded for"
date: 2026-11-23
status: "Accepted"
summary: "A server's omnibot recording can name its prompts. It then plays a run only for a message that holds one, refuses any other with a typed 422 before touching the conversation, and the conversations list reports live, replay or none with the recorded prompts, so the console says it is a recorded demo and offers them"
refs: [2443, 2121, 2163, 2298]
---

- **Date:** 2026-11-23 | **Status:** Accepted | **Pages:** [AI](/architecture/ai/), [connect an agent](/developer/agents/#omnibot-in-the-console)
- **Decision:** (#2443, on the recording of [ADR-2121](/architecture/decisions/#adr-2121-the-server-runs-omnibot-as-the-person-streaming-ag-ui).)
  1. **The recording says what it answers.** A recording's top-level `prompt` names the question its
     turns answer, and a conversation may carry a `prompt` beside, or instead of, its `match`. A
     prompt matches a message however it is cased, spaced or punctuated. A recording with no
     top-level `prompt` plays its turns for any message, as before, so tests and tapes are unchanged.
  2. **The server refuses the rest.** A run whose message no script answers is a 422 of type
     `urn:omniglass:omnibot-replay-unmatched`, "This lab plays a recording; connect a model to ask
     your own question", before the conversation is claimed, so nothing is stored or run. Matching is
     decided where the recording is, never by a client comparing strings.
  3. **A client can ask which it is talking to.** `GET /assistant/conversations` gains an `omnibot`
     object: `mode` (`live` for any configured model, `replay` for a recording, `none`) and the
     recording's `prompts`. Nothing of a model's configuration is told. The console shows a Recorded
     demo note and the prompts as chips only in replay.
- **Why:** a lab with no live model played the recording's default turns for any question, so
  "Which alarm is worst?" read an unrelated component and proposed a tag with no answer: a wrong
  answer, not a replay. Refusing an unrecorded question, and saying why, keeps the demo honest.
