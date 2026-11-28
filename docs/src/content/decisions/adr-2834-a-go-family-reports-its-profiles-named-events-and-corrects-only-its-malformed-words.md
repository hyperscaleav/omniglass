---
title: "A Go family reports its profile's named events on the live feed and corrects only the profile's malformed words"
date: 2026-11-28
status: Accepted
summary: "A family publishes the named occurrences its profile reported (a call started, a log line) from a route or a tick, in the same numbered stream as the state changes the feed streams, and the recorded transcripts hold them byte for byte. The Kestrel families correct three things the profiles got wrong or spelled differently: a log line that printed a formatting error, a value written to state as the word it was sent, and a tick's interval listed in seconds."
refs: [2834, 2826, 2832, 2829]
---

- **Date:** 2026-11-28 | **Status:** Accepted | **Pages:** [simulation](/architecture/simulation/)
- **Decision:** A Go family reports a named occurrence on its device's live feed with `Device.Publish`,
  from a route after the writes it follows, or from a tick's `Emit`, which reads the device after the
  tick's changes are written (as a profile behavior's events are rendered against the state its
  mutations left). The recorder logs those events beside the state in every transcript, so the
  VRoom's `call-started`, `call-ended`, `participant-joined`, `participant-left` and `log` events are held
  to the profile's, in order, for every request, seeded tick and room day. A route marked `Private` is
  listed as private. Three differences from the Kestrel profiles are written down and pinned in the
  parity tests, and nothing else differs. The bar's uplink-recovered log line printed the loss with a verb
  its template could not format when the loss was exactly zero, so it read `loss %!f(int64=0)%`; the
  family writes `loss 0.0%`. A value a route could not read as its kind (a volume or a pan that is a word,
  a mute that is a number, a missing field, a dialed codec's count that is not a number) was written into
  state as text and answered a success; the family refuses it with a 400 and writes nothing, the same
  correction ADR-2832 made for the Boreal controls. The console lists a tick's interval in whole seconds
  as it does for every family, so the status line's `15m` reads `900s`.
- **Context:** The Kestrel VRoom is the course's one device whose feed carries named events beside state
  changes, and a driver maps each event to one of its own, so "readings stay identical" (#2826) includes
  them. A profile's route events and behavior events were the only way to publish one, so a family had no
  way to keep them until the feed was reachable from Go. Recording them with the wire and holding them in
  the replay is cheaper than reasoning about each. The recorded zero-loss line is the one place the
  profile's own words were wrong rather than its template's accident, and a collector that reads log text
  would have been handed `%!f(int64=0)` for as long as the profile lived.
