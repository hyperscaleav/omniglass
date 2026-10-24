---
title: "One word for the machine identifier: Key"
date: 2026-08-04
refs: [553]
form: bullet
---

- **One word for the machine identifier: Key** (#553). The console had settled on `Segment`, which
  named a part where the whole is not visible: on a vendor form there is no path on screen, so the
  word was precise for us and meaningless at the point of use. It also hardened a migration artifact
  into vocabulary, since `Key` and `Segment` were never two concepts but one concept in two states, an
  entity key already kebab and a keyspace key still snake pending the address grammar.

  `internal/key` had fixed the right doctrine long before this slice and nobody had noticed: a segment
  is one dot-separated component of a key. So a key is a value and a segment is a position, which
  makes "segment" correct in prose about topic structure and wrong on a form. The console now says
  `Key` everywhere, `identity-vocabulary-guard.test.ts` retires `Segment` alongside `Display name` and
  `Technical name` as label text (matching quoted strings and JSX text only, so a donut chart may keep
  its segments), and `ValidateSegment` became `ValidateEntityKey` in `entity_key.go`, which no longer
  collides with the `segment` regex `internal/key` already owned.

  Two key rules remain, deliberately: an entity key is kebab and a keyspace key is snake with an
  optional dot hierarchy. They are not merged, because each legitimately carries a character the other
  forbids. The console says `Key` for both, and the differing character set surfaces as a validation
  message rather than as a second word.
