---
title: "A simulated system is built to a standard, with platform-minted names"
date: 2026-10-18
status: "Accepted"
summary: "A simulated system is built to a standard: each role staffed with a simulated product that fits, short roles listed, names platform-minted, all in one transaction; the lab archetypes are not an operator concept"
---

- **Date:** 2026-10-18 | **Status:** Accepted | **Pages:** [simulation](/architecture/simulation/), [API](/architecture/api/)
- **Decision:** Adding a whole simulated system builds it to one of the platform's standards. Each of
  the standard's roles is staffed with the first simulated product that fits its accepted types and
  pins (quorum-many, at least one, capped at capacity; one alternate per choice, the first fully
  staffable); a role nothing simulated fits is left short and listed with the reason. Components are
  named as an unnamed create names them. The system, components, endpoints and role assignments land
  in one transaction, each through the ordinary create with the caller's scope and audit row. The lab's
  room archetypes (`internal/sim/profiles/systems`) are not an operator concept; they remain only as
  the simulator's internal fleet compiler input (#914).
- **Context:** #911 proposed adding a system from the lab's archetypes. Their roles and component mixes
  did not match the platform's standards (a "codec" slot holding a video bar, products with no seeded
  counterpart, display and DSP mixes no standard declares), so a mapping table would have kept two
  vocabularies for one idea. The architect ruled the standard is the archetype and names follow the
  platform's own minting.
