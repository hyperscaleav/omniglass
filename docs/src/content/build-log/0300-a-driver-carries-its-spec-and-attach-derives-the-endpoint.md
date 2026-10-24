---
title: "A driver carries its spec, and attach derives the endpoint"
date: 2026-10-17
refs: []
form: bullet
---

- **A driver carries its spec, and attach derives the endpoint.** The driver registry rows gain
  their body: a versioned declarative spec (data, not code: one transport, typed inputs with
  secret references, poll functions, listeners, command bindings), parsed strictly (an unknown
  field refuses rather than projects) and validated against the live catalogs at every write, so
  an unregistered emitted name or a phantom secret type refuses at authoring time with the
  fault named (ADR-0135). Creating an endpoint with a driver reference becomes an attach: the
  spec derives the transport and target, required inputs must be supplied and defaults bake,
  secret inputs resolve by reference name against the secret store (a missing row or the wrong
  shape is a 422 naming it), and the spec's functions derive the endpoint's tasks in the same
  transaction, each carrying its function whole with emit lanes baked at attach so the node
  never consults a catalog at runtime. The seeded `snmp-generic` (v2c scalar: sysDescr and
  sysUpTime, timeticks scaled to the canon's seconds by a declared transform) and `newtron-nvp`
  (line protocol: a status poll, an EVT listener with its subscribe arm, a set-input binding)
  become real specs, `uptime` joins the metric canon, and the seed installs secret types before
  drivers so a first boot validates cleanly. The console's endpoint create grows the attach face
  (pick a driver, fill the inputs its spec declares) beside the bare probe, the Drivers page
  renders each spec as the menu it is, and the docs' driver table renders from the same generated
  facts. Proven by pure spec-validation units, testcontainer round-trips over the write gate and
  the attach flow (derived tasks, lanes, refusals), and an HTTP e2e that attaches the seeded
  driver (#813, #603).
