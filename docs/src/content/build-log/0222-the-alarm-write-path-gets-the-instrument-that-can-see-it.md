---
title: "The alarm write path gets the instrument that can see it, and it is not flat"
date: 2026-08-13
refs: []
form: bullet
---

- **The alarm write path gets the instrument that can see it, and it is not flat**
  ([#674](https://github.com/hyperscaleav/omniglass/issues/674)). The recompute behind every alarm the
  platform raises was the one hot path with a two-digit statement count and nothing measuring it. A
  wall-clock benchmark of it was written during [#651](https://github.com/hyperscaleav/omniglass/issues/651)
  and deliberately dropped: against the measured 262 us round-trip floor it is about three-quarters
  transport, so doubling every query plan inside it would move the number by ~25%, barely above its own
  spread. Counting is the instrument that does not care that transport dominates.

  The issue named two candidate growth dimensions, the number of alarms and the number of roles the
  component fills, and asked which one the code actually varies over, because a fixture that holds the
  growing dimension constant is flat by accident and this repo has made that mistake twice. Measured
  over seven fixtures, two of them held back as out-of-sample predictions, **one closed form fits all
  seven and predicted the last two before they were run**:

  ```
  raise = clear = 12 + 5*S + 4*L
  ```

  S is the number of slots the component fills, L the number of distinct locations above the systems
  holding them. So the first candidate is **false** and the second is **true**. The count is flat in
  the number of alarms, because a component's open alarms are folded by one `array_agg`; it grows at
  five statements per staffed system (an advisory lock, a role resolution, and a recorded verdict that
  re-takes the lock) and four per ancestor location. The 58 the issue reports is this shape's minimum,
  one system three locations, counted as the raise and the clear together.

  Both facts ship as assertions, and the shape of each follows what is true rather than what would look
  tidy. The flatness in alarm count is asserted with an explicit guard that the two measurements were
  taken over genuinely different open-alarm sets, since a fixture that quietly deduplicated its alarms
  would report a beautifully flat number while varying nothing. The growth is **pinned as an equation**,
  the way [#671](https://github.com/hyperscaleav/omniglass/issues/671)'s defect was pinned before it was
  fixed, so a change is reported as which term moved; a ceiling generous enough to look comfortable
  would have hidden the slope underneath it. Reducing the count is explicitly out of scope: a reduction
  with no measurement in front of it cannot be shown to have worked, and this is that measurement.
