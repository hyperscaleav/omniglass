---
title: "A Go family keeps its profile's wire byte for byte and corrects only the profile's accidents"
date: 2026-11-27
status: Accepted
summary: "A family that replaces a YAML profile answers every request the profile answered with the same bytes, headers and state, proven against transcripts the profile recorded before it was deleted. Two accidents of the profile's templates are corrected, each listed in the parity test: a product reports its own model on its dashboard, and a control route refuses a body it cannot read."
refs: [2832, 2826, 2829]
---

- **Date:** 2026-11-27 | **Status:** Accepted | **Pages:** [simulation](/architecture/simulation/)
- **Decision:** A Go family that replaces a YAML profile is held to the profile's wire, not to its
  author's intent. Before the profile is deleted a recorder plays a fixed script against it (every route
  valid and refused, SNMP gets and walks, the drift a seeded source draws, a pinned room day with and
  without a calendar) and commits the transcript as testdata (`sim/testdata/wire`). The family's parity
  test replays the script and compares status, headers, body bytes, the state after each write, SNMP
  types and values, and the behavior sequences. The family is correct when it matches, and a difference is
  either fixed or written down as a divergence in the test. Two are written down for the Boreal families
  (#2832). A product reports its own model: the Edge dashboard named "Edge 86" in its title and bar for
  every size because the template said so, and each size now says its own, so a 98-inch reports itself as
  one. A control route refuses a body it cannot read: the profile wrote `<no value>`, the word "loud" or
  the number 7 into a brightness, a volume or a mute and then answered a document that is not JSON, and the
  family answers 400 and writes nothing. A value outside a route's fixed list is refused as before, with
  the engine's own validation body.
- **Context:** The epic's bar is that readings stay identical (#2826). A profile's tests assert what its
  author thought of, and the template engine adds behavior nobody wrote down (a missing field renders as
  `<no value>`, a JSON number reaches state as an int or a float by its look). Recording the profile's own
  answers is the only complete statement of what "identical" means, and it costs nothing once the profile
  is gone. The two corrections are the only places the recording would otherwise have enshrined a bug:
  a size reporting another's name, and state a collector would read back as garbage.
