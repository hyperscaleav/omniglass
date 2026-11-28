---
title: "The census, the PDU and the cloud services are families of one named for their product, and the profiles' template accidents are refused plainly"
date: 2026-11-28
status: Accepted
summary: "Each of the seven profiles has its own wire API, so each is a family, named for its only product so the lab's pushers, senders and manifests resolve it unchanged. The families keep the profiles' wire byte for byte except where a response was the template engine's accident: error text, text coerced into numbers, markup printed into a page."
refs: [2836, 2826, 2832, 2833]
---

- **Date:** 2026-11-28 | **Status:** Accepted | **Pages:** [simulation](/architecture/simulation/)
- **Decision:** One package per wire API (ruling 1 of #2826). The seven profiles share no API (a people
  counter, a PDU, an SNMP box and four cloud services behind different headers and routes), so there are seven
  families of one row. Each is named for its product, which the family table now allows (a model row may carry
  the family's own name): the lab's manifests, pushers and senders name a product as a profile
  (`booking-service`, `ucc-platform`), and a rename would touch every one. Parity with the profiles is held by
  golden transcripts recorded before they were deleted (ADR-2832), now also covering the census's raw-TCP and
  binary bands, the recording service's event stream, and every webhook post the UCC platform makes, checked
  against its signature. What differs, each pinned in the replay:
  - A refusal that was the template engine's own text (a failed guard or path segment: Go template errors and
    the state path with `{{ }}` in it) is a plain 400 that writes nothing, naming the field and its pattern.
    A refusal the engine wrote as a validation body is unchanged, byte for byte.
  - Text is stored as text. The profile coerced a value that read as a number or a flag into one: a
    recording id `"12345"` became the number 12345, a webhook target of `5` or `true` was stored. A field
    that is not a string is refused 400 (`Quirk`).
  - The UCC dashboard names a room by its id when the platform holds no name for it, and reads a room with no
    state as idle, as its room list always did, where the template printed the markup `<no value>`; every
    page is `html/template`, so what a device holds is escaped.
  - A body that is not a JSON object is refused as one (ADR-2832).
- **Context:** The epic's bar is that readings stay identical (#2826). The recording found the divergences
  above where the profile and its author's intent part; nothing a collector, a driver or a lab test reads from
  a valid request changes, and the five lab tests that read these devices run unchanged.
