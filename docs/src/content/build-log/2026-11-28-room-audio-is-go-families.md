---
title: "The Lyra microphones and the Polaris DSP and amplifier are Go families (#2835)"
date: 2026-11-28
refs: [2835, 2826, 2832]
---

The room's audio chain left YAML: the Lyra Arc A2 ceiling microphone and HX-1 wireless system, the
Polaris DSP 16 and the Amp 4-100 are four Go families under `sim/device/` (`lyraarc`, `lyrahx`,
`polarisdsp`, `polarisamp`), and their four profiles and every template behind them are deleted.

The grouping follows the wire API, and where the seed collects two products differently they are two
families, as the catalog test requires: the Arc and the HX-1 speak the same small API (a status and a
mute behind the Lyra key, mute and level on the Lyra SNMP arc), but they are different kinds of device
with their own driver, so each is its own family over one shared body (`sim/device/internal/lyra`),
each a one-row table with room for the next model. The DSP and the amplifier are one family each.

Proof came first. Before anything was deleted a recorder played one script per product against the
YAML profile (every route valid and refused, the SNMP reads and walks, the DSP's SSH, Telnet and
raw-TCP shell sessions with their pushes and its UDP echo, the drift each seeded source draws, and a
pinned room day with and without a calendar) and the transcripts were committed
(`sim/testdata/wire`). The families replay them: the same bytes, headers, state, SNMP types and
values, shell output and sequences, and the same console listing of every face, with only the
accidents of the templates refused instead
([ADR-2835](/architecture/decisions/#adr-2835-a-mixer-channel-the-profile-addressed-by-accident-is-refused)).
The generic wire harness grew to hold a family with line faces: it records a family's faces, plays a
script over each persistent face through the shell loop all three transports run, and sets state a
scenario would. New shared pieces, each its own commit: `httpapi.Switch` and `httpapi.Element`
(a true/false enum, and a control on one element of a list named by the body), `httpapi.Sections`,
`TickContext.Spans` (the room's meetings over a window, for the pack's battery), and a Go subject's
`PushLine` (a push line that names the subject and token it was subscribed with).
