---
title: "Transport becomes code, and the entity an API is reached through is an endpoint"
date: 2026-10-17
refs: []
form: bullet
---

- **Transport becomes code, and the entity an API is reached through is an endpoint.** ADR-0073
  executes and #603's naming ruling rides it (ADR-0134): the six wires (icmp, tcp, udp, ssh, http,
  snmp) are a code registry the binary ships (`internal/transport`, a leaf package so the storage
  gateway validates against it without an import cycle), the `interface_type` table and its FK
  retire, and the entity renames end to end in one slice. The migration carries everything that
  spells the old noun: the table (with the task arc's column and every constraint code names),
  role permission arrays rewritten in place preserving order, and the canonical reachability
  datapoint renamed row-in-place so every uuid-keyed sample keeps its history; a testcontainer
  round-trip stands the database below the migration, plants pre-rename rows, and drives the
  embedded SQL twice for idempotence, with the down exercised by the same test's rollback. An
  unknown transport now refuses at write against the registry (422) instead of surfacing as a
  null-FK insert error; `GET /transports` serves the registry to the console picker (bound
  through `bindSelectValue`, since the options arrive after the blade opens), so no surface
  hardcodes the pair the old picker showed. The docs corpus, the operator and CLI guides, the
  glossary, and the seed catalogs speak endpoint; `interface_type`, `interface-reachable`, and
  the entity phrasings of the old noun join the docslint denylist, which promptly caught five
  stragglers, the seeded datapoint's own YAML among them (#811, #603).
