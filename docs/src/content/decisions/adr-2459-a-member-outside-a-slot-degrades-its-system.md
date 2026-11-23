---
title: "A member outside a slot degrades its system, and an occupant still to attach reads incomplete"
date: 2026-11-23
status: Accepted
summary: "Any open alarm on a member that fills no slot makes its system degraded, whatever the alarm's severity, never outage: slots alone decide outage. A slot whose occupants are still to attach (a driver product with no endpoint) reads incomplete. The place roll-up is unchanged and carries the new system health."
refs: [2459]
---

- **Date:** 2026-11-23 | **Status:** Accepted | **Pages:** [the model](/start/the-model/#judging), [health](/architecture/health/#the-chain-a-components-own-health-is-the-routing-key), [glossary](/architecture/glossary/), [alarms and actions](/architecture/alarms-actions/), [Explore](/operator/fleet/)
- **Decision:** (#2459, ruled by the architect in the console review)
  1. **A member outside a slot degrades its system.** A system's health is the worst of its slots'
     contributions and, new, of its members that fill no slot there: any open alarm on such a
     member contributes `degraded`, at any severity. It never contributes `outage`: only a slot
     declares what losing a component costs the room, so only a slot can say the room cannot run.
     The alarm counts exactly as it counts for a slot's occupant (open means not cleared;
     acknowledgement and snooze do not change it). A hand-built system (no standard, so no slots)
     is therefore no longer healthy forever.
  2. **An occupant still to attach reads incomplete.** A component whose product is collected
     through a driver and which has no endpoint yet still occupies its slot (nothing has failed), but a
     slot whose attached occupants fall short of quorum reads `incomplete`, the commissioning gap a
     slot nobody staffed already reads. A system started from a template with components still to
     attach reads incomplete rather than healthy. A component whose product has no driver (a passive
     speaker) has nothing to attach and is never pending.
  3. **The place roll-up is unchanged.** A location takes the worst of its children's recorded
     health as before; it carries the new system health with no change of its own.
  4. **The health read explains it.** `GET /systems/{name}/health` lists every member with its own
     health, its open alarms, whether it fills a slot (`outside_slot`) and whether it is still to
     attach, plus a `reason` line computed from the same inputs as the health (for example "1 member
     outside a slot has an open alarm (amp-1)"). The reason is computed on read; the recorded
     transition stays the health value alone.
  5. **Every write that can move it recomputes.** Binding and unbinding a member, deleting a member
     component, attaching and removing an endpoint, and changing a component's product now recompute
     the systems the component belongs to. A boot step recomputes, once, every system the new rules
     can have moved while the fleet ran an earlier release; recording is transition-only, so a
     current fleet writes nothing there.
- **Context:** A component of a hand-built system could be in outage while its system, every place
  above it, Explore's attention count and Home all read healthy, with nothing saying why (#2459). The
  previous rule, that a component filling no slot moves nothing above it, kept slot impact the one
  way a system's health moved, which is right for outage and wrong for silence.
- **Consequences:** On upgrade, an installed fleet's systems with an alarmed member outside a slot
  turn degraded, and systems whose slots hold only components still to attach turn incomplete. The
  platform system (whose members are the outposts and the server, in no slot) now reads degraded
  while one of them holds an open alarm. The alarm write path costs one more statement per system it
  touches (the members outside a slot, read in one statement however many). A product's driver being
  added or removed in the catalog does not recompute the components of that product; the reads stay
  right, and the history catches up at the next write to each system.
- **Not chosen:**
  - **Display only.** Listing the alarm on the system while its health stayed healthy would have left
    places, Explore and Home reading healthy over a broken box.
  - **Let the member's severity through.** An outage member outside a slot making its system an
    outage would let a spare cable tester take a room down; impact belongs to the slot.
  - **Never observed reads incomplete.** Using the `unknown` reading (nothing has ever observed the
    component) would have needed a recompute on a component's first ingested batch, on the ingest
    path, and would have marked passive components forever.
