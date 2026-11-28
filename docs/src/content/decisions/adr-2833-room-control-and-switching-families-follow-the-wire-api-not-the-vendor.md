---
title: "Room control and switching split into five families by wire API, and refusals that were template accidents are refused plainly"
date: 2026-11-28
status: Accepted
summary: "The five Newtron and Meridian profiles become five families, one per wire API, not one per vendor: a processor, a touch panel and a scheduling panel answer different NVP lines, and a switcher and an extender answer different JSON. The families keep the profiles' wire byte for byte except where a refusal was the template engine's own text, which is refused as a 400 that writes nothing."
refs: [2833, 2826, 2832]
---

- **Date:** 2026-11-28 | **Status:** Accepted | **Pages:** [simulation](/architecture/simulation/)
- **Decision:** One package per wire API (ruling 1 of #2826), so the Newtron line is three families and the
  Meridian line two, each a one-row model table today. `newtroncore` answers the touch panel's room and its
  own program, ports, panels, room power, temperature and macro; `newtronpanel` answers the room on five
  faces (raw TCP, TLS, Telnet, UDP and an SSH shell) beside its web API and a deliberately dead SNMP agent;
  `newtronslate` answers a room's booking alone; `meridianswitcher` takes a route; `meridianextender` has a
  status and a reboot and no route. A processor and a touch panel share the NVP room surface and nothing
  else, and the catalog's alignment test already holds a family's models to one driver and one kind of
  component, which these five could not satisfy as fewer. Parity with the profiles is held by the golden
  transcripts recorded before they were deleted (ADR-2832), now covering line protocols: every NVP and
  shell line with its reply and the pushes it draws, and the device's own command list.
  Two refusals differ from the profiles, each marked `Quirk` in the recorded script and pinned in the
  replay as a 400 that changes nothing:
  - A route whose field the profile wrote through a template without checking it (the panel's scene,
    source, brightness and log line, the switcher's route) answered, for a missing field or one that was not
    text or a number, with `<no value>` written into state or with the template engine's own error text.
    The families refuse it as a control route refuses any unreadable field (a 400 naming the field), and a
    string stays a string: the profile coerced `"7"` or `"true"` into a number or a flag.
  - The switcher's refusal of an input it does not have named a second path, the signal on that input, which
    the template resolved to `<no value>`; it names the input alone.
- **Context:** The epic's bar is that readings stay identical (#2826). Recording the profiles found these
  where the profile and its author's intent part: no collector reads `<no value>`, and the console lists
  the quick sets of the input route as the one choice it is. Everything a collector, a driver or a test can
  observe of a valid request is unchanged, including the accidents that are harmless (the panel's
  room, source and brightness routes all acknowledge the room's scene; the scene and source accept any
  text, as the profile did).
