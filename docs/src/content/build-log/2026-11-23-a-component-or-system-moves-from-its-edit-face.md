---
title: "A component or system moves from its edit face, and Also serves stops moving it (#1804)"
date: 2026-11-23
refs: [1804, 767, 879]
---

The edit face of a component and of a system carries **Move to...** under Placement, wired to the
`:move` routes (`component:move` / `system:move`): another place, under another component or system,
and for a component another system (and optionally a slot in it). A move runs on its own button, not
on **Save**, shows the server's refusal in place (a cycle, a destination out of scope, a full slot),
and leaves the page on the entity, with the fleet view, the entity lists, the memberships and the
slot lists refreshed.

The Memberships field used to be a single select: choosing another system added a second membership
and silently made it primary, so it read as a move and was not. It is now three explicit controls:
**Also serves...** adds a system and leaves the primary alone, **Primary system** chooses among the
systems the component is in, and each chip removes one. Moving a component between systems is the
planned sequence in `lib/members.ts` (`planSystemMove`): join (staffing the chosen slot), make
primary, free the slots it held, leave.
