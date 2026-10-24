---
title: "The console stops answering a question only the operator can answer"
date: 2026-08-13
refs: []
form: bullet
---

- **The console stops answering a question only the operator can answer.** A command type naming a
  target arm is asking to be settled against a reported value, so its settle window decides when a
  difference becomes a verdict. The create form seeded that field with `0` and folded it with
  `Number(settle()) || 0`, so an untouched field, a typo and a deliberate zero all reached the
  server as the same explicit `0`: a settleable type judged at the instant of issue, which nobody
  chose and no refusal announced. The value was never the defect. Zero beside a target is a
  statement of intent, the documented way to say "judge it now"
  ([ADR-0108](/architecture/decisions/#adr-0108-settlement-reads-one-clock-and-a-zero-window-is-a-statement-of-intent)),
  and refusing the combination at the gateway would have reversed a decision accepted the day
  before and broken its own clock regression test, whose observable does not exist at a positive
  window. The default was the defect. The field now starts blank on the create form and the window
  is read through one rule both write surfaces share: a settleable type must state its window, and
  the create and the blade's Save are refused until it does; a fire-and-forget type sends no window
  at all and takes the column's own 0, which is `reboot`'s shape unchanged. Unstated survives as
  far as the body, where it is a field that is simply not sent, so it can no longer be confused with
  a chosen zero. What a stated zero does is now legible where it is typed, next to the field rather
  than only in the API reference, and a typed negative is refused there too: `min="0"` had been on
  that input since the command pillar landed and refuses nothing, because both submit paths are
  JavaScript (the drawer's action bar and the blade's Save sit outside any form), so native
  constraint validation never runs on the path an operator uses. The filed issue's own account of
  the symptom was wrong and is corrected with it: a zero-window settleable command is `settled` when
  the observed value already matches, `failed` when it differs, and `timed-out` only when nothing
  has been observed, not `timed-out` unconditionally.
