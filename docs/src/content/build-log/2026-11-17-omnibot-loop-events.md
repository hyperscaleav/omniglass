---
title: "omnibot's loop reports each step and can pause at an ask (#2120)"
date: 2026-11-17
refs: [2120, 1161]
---

omnibot's loop now reports every step through one event hook: the model's text, each call it proposes
(by the provider's call id, with its arguments and the tool's policy), the decision, and the result the
model is told. Before, the hook saw only a decided call, with no id or arguments, and a streaming client
had to wrap the model to see its text. A loop set to pause stops at an `ask` and returns the
conversation with the call it waits on; `Continue` resumes from that conversation with the owner's
decision, finishes the turn's calls in order, and runs on. The tests drive a recorded model: a read
then an ask emits its events in order, a paused run continued with an approval ends in the same
conversation an inline approval produces, and a declined continue writes nothing. `omniglass omnibot`
still asks inline and prints the same line per call.
