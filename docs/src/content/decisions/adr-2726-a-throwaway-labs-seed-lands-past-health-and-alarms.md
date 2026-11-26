---
title: "A throwaway lab's seed lands past health and alarms, labelled as seeded"
date: 2026-11-26
status: Accepted
summary: "A throwaway lab started with --health-history lands 30 days of health edges and the alarms that explain them, raised and cleared in the past, through two lab-only Storage Gateway writes that stamp the past, label every row source 'devseed', write no audit or event row, recompute nothing and notify no one. The alarm row gains a source column for it. An exception to derived state being only the platform's own write, until epic #1796 derives lab history through the real rules on a virtual clock."
refs: [2726, 2727, 1796, 1790]
---

- **Date:** 2026-11-26 | **Status:** Accepted | **Pages:** [health](/architecture/health/#health-is-recorded-as-a-transition), [alarms and actions](/architecture/alarms-actions/#storage), [the lab](/developer/lab/)
- **Decision:** (#2726, approved by the architect.) A throwaway lab's seed may land past derived state
  directly. `LandSeededHealth` writes a system's (or a component's or location's) health edges as the
  calculated `health` rows the health read serves, at the given past times; `LandSeededAlarms` writes
  alarms already raised and cleared, keyed as their rule would key them. Both label every row with the
  seed's source (`devseed`, as `--platform-history` labels its samples, #1790), are their own sentinel
  (a second landing writes nothing), write no audit or event row, recompute no health and queue no
  notification. The `alarm` row gains `source` (empty for every alarm the platform raises) so a seeded
  alarm is labelled like a seeded health row. The lab plan is the guard: the writes run only from
  `lab seed --health-history`, which needs a throwaway (`--ephemeral`) lab.
- **Context:** A fresh lab's health began minutes before a capture, so a one-minute outage swung a
  room's Uptime from 82% to 69% (the marketing teaser, #2722), and nothing read like a fleet that had
  been running. Health and alarms are otherwise only ever the platform's own writes, stamped now
  ([health is recorded as a transition](/architecture/health/#health-is-recorded-as-a-transition)), and
  that stays true of every instance that is not a throwaway lab. Generating the history through the real
  rules on a virtual clock is epic #1796, several slices larger; this hand-shaped cut is labelled so
  #1796 can retire it. A seeded alarm's own timeline is empty (no events, no audit rows), and it counts
  as nothing having observed its component, so a component nothing collects from still reads unknown. The
  seeded health goes before each owner's own record and never rewrites it: the health series holds that
  no value follows itself and that id order is time order (every live read takes the highest id), so the
  owner's series is rebuilt under its health lock with the seeded edges first and its own rows copied
  after them, and an edge at or after the owner's first recorded one is refused.
