---
title: "Shared protocol surfaces are Go helper packages (#2831)"
date: 2026-11-27
refs: [2831, 2826]
---

The protocol pieces several simulated devices answer alike now have Go declarations under `sim/surface`,
the first step of moving the catalog from YAML profiles to Go families. `surface/nvp` is the Newtron room
command set and its subjects, `surface/snmp` the MIB-II system group and the vendor identity arc nine
profiles repeat, and `surface/httpapi` the `GET /api/status` document whose identity block ten profiles
repeat. Each takes typed accessors and returns typed replies, OIDs and responses, with no template string.

A parity test per helper serves the catalog's own YAML and a minimal Go declaration through the same
protocol server and compares the bytes: a raw TCP transcript (commands, refusals, subscriptions and
their pushes), a GETNEXT walk and GET of every OID on a pinned clock for every profile with an SNMP face,
and the status document against six real templates. No device is converted and the YAML surfaces keep
working until their last user moves.
