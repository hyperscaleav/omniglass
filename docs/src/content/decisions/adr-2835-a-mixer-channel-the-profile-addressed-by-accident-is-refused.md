---
title: "A mixer channel the profile addressed by accident is refused"
date: 2026-11-28
status: Accepted
summary: "The DSP's channel routes built a state path from the request body, so a fractional index wrote a key no channel has and a mute or gain that was no flag or number was written as the word. The family refuses both with a 400 that changes nothing, and answers every other bad index in the words the state gives."
refs: [2835, 2832, 2826]
---

- **Date:** 2026-11-28 | **Status:** Accepted | **Pages:** [simulation](/architecture/simulation/)
- **Decision:** The Polaris DSP family (#2835) keeps the wire of the YAML profile it replaced, proven by the
  transcripts the profile recorded ([ADR-2832](/architecture/decisions/#adr-2832-a-go-family-keeps-a-profiles-wire-byte-for-byte-and-corrects-only-the-profiles-accidents)),
  and corrects the accidents of its templates, each listed as a `Quirk` step in the audio wire script. The
  channel routes (`POST /api/channels/mute`, `POST /api/channels/gain`) took `status.channels.<index>.<field>`
  from the body: an `index` with a decimal point (`1.5`, `"2.5"`) wrote a key that is no channel, nested inside
  the channel its whole part names and listed by every later read of it, and a `mute` or `gain` that was a word, a number where a
  flag belongs, missing or null was written into the channel as that word, `<no value>` or `<nil>`. The family
  refuses both with a 400 that writes nothing: an index must be a whole number, a mute a boolean or its text, a gain a
  number or its text. A body that is no JSON object is refused as unreadable, as every control route
  refuses it (ADR-2832). The other bad indexes (negative, past the last strip, a word, missing, null, a boolean,
  empty) are not accidents but refusals, and stay byte for byte: the 400 `mutation failed` document, naming the
  path and the state's own reason (`index 16 out of range [0,16)`), which `httpapi.Element` builds from the list's
  length.
- **Context:** Readings stay identical (#2826), and recording the profile's own answers is the only complete
  statement of what identical means. Most of what the recording held was a wire to keep. These requests were the
  exceptions: a collector could read back a channel that held the word "loud" as its gain, or a channel with a stray
  key, and nothing a lab operator typed into the console's operations panel could have meant either. Refusing them
  costs a 400 where a malformed request used to succeed, and every request the console and the profile's own
  dashboard make is unchanged.
