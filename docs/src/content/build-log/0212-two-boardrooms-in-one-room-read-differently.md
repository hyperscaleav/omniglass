---
title: "Two boardrooms in one room read differently"
date: 2026-08-13
refs: []
form: bullet
---

- **Two boardrooms in one room read differently.** A divisible boardroom is two `board` systems in
  one room, and the shipped system rule read the type alone, so both rendered "Boardroom": the
  platform could tell them apart (`boardroom` and `boardroom-2`, ADR-0101's suppression) and the
  operator reading the console could not. The rule now reads the ordinal under the component's own
  `{{if .Ordinal}}`, and the estate's two halves are the demonstration rather than two pinned labels
  spelling A and B over the top of it. What the issue and the ruling both described as one change was
  two, and the second is where the argument sits: the rule alone renders "Boardroom 1" for the only
  boardroom in a room, because `{{if}}` is false for an EMPTY string and a suppressed first name
  still owns the stored ordinal 1. So the data map's `Ordinal` became the number the row's NAME
  shows, asked of the mint rather than read off the name, which makes a label and a name unable to
  disagree about how many of a thing there are and costs the ability to author "Boardroom 1" for a
  system called `boardroom`, a string this arc already calls the defect it was filed about. Every act
  that moves a system's ordinal was re-derived and the set grew by none, because each of them already
  restamps unconditionally; the estate-wide invariant now runs a system rule that reads the ordinal,
  where the one before it could not have seen a hole in any of this. A shipped rule change restamps
  nothing, so an existing estate keeps both halves reading "Boardroom" until an operator runs
  `/systems:recomputeLabels`, which the seed, the architecture page and the operator-facing rule text
  each say where they are met
  ([ADR-0101](/architecture/decisions/#adr-0101-the-first-of-its-stem-in-a-bucket-carries-no-ordinal-and-the-mint-that-says-so-is-the-one-allocation-tests) amended).
