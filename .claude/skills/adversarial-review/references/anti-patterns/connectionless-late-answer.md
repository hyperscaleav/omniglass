## connectionless-late-answer (fixed; found by the #1037 review)

- **Cue:** an exchange over a connectionless transport (a udp datagram) that re-sends, or that
  takes a reply of any shape, with nothing that absorbs the replies still on the way once it
  settles.
- **Failure:** a component slower than one attempt's share answers every send, so a poll settled on
  its second send leaves its first reply in flight; the command sent next, with the same reply
  shape, took the poll's `INPUT hdmi1` as its own answer, and settlement judged it against the
  wrong value. A held line connection recycles itself for this; a datagram socket has nothing to
  recycle.
- **Counterpart:** a surplus guard keyed only on "settled after a re-send" eats genuine answers on
  a lossy wire: a lost send draws no reply, so the guard absorbed the next ask's identical ack and a
  command sent once read failed although it acted (found by the #1037 re-review).
- **Fix:** every udp request declares its reply's shape (refused at write otherwise,
  `validateRetries`, internal/driver/spec.go), and only an answer that came back too soon after the
  last send to be its reply (sooner than half the latency learned from sends answered at once)
  leaves a ghost absorbing repeats of it for one more timeout (`datagramGhost` and `learn`,
  internal/collection/udp.go; `TestDatagramSurplusRepliesNeverAnswerTheNextAsk`,
  `TestDatagramLossLeavesNothingToAbsorb`).
