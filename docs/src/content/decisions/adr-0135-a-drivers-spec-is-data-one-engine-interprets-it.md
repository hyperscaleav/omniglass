---
title: "A driver's spec is data, one engine interprets it"
date: 2026-08-28
status: "Accepted"
summary: "A driver's body is a versioned declarative spec (jsonb on the driver row, validated against the catalogs at every write): one transport, typed inputs with secret references, and three function families (polls, listeners, command bindings). Attach derives the endpoint and its tasks from the spec, emit lanes baked at attach; a listener's arm conversation is session plumbing, never recorded command rows"
---

- **Date:** 2026-08-28 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/),
  [glossary](/architecture/glossary/)
- **Decision:** A driver's body is a **versioned declarative spec** (`jsonb` on the driver row,
  `version: 1`, parsed strictly so an unknown field refuses rather than projects) interpreted by
  one generic engine, never per-driver code: the one transport it rides, typed inputs (`string`,
  `number`, `secret` with a `secret_type`, supplied as a reference name and resolved at attach),
  and three function families: poll functions (schedule, transport-shaped request, per-emit
  extraction with declared transforms: cast, scale, enum map), listeners (an `arm` conversation,
  a match rule, the same extraction), and command bindings (how a `command_type` actuates). Each
  emit names its sample in a lane catalog (the ADR-0065 vocabulary: the name is the signal, what
  lands is a sample). The
  spec validates against the live catalogs at **every write** (create, update, boot seed; the seed
  now installs secret types before drivers so a first boot validates cleanly), so an
  uninterpretable spec refuses at authoring time with the fault named. **Attach derives, never
  authors**: an endpoint created with a driver reference takes its transport and params from the
  spec, and its tasks derive in the same transaction (a poll task per poll function, a standing
  listen task per listener), each task carrying its function whole with **emit lanes baked at
  attach**, so the node never consults a catalog at runtime. A listener's `arm` lines are session
  **plumbing** re-sent on every recover (ruling 3 on #603), never recorded command rows: the
  command lane records operator intent with settlement semantics, and machine chatter would flood
  it.
- **Context:** #813 (epic #603): the driver registry rows existed with no body, and the epic's
  produce path needs the menu before the interpreter. Data-not-code keeps every driver inspectable
  and diffable, keeps validation one function rather than a plugin ABI, and matches the template
  authoring model (#489) this layer will slot under. The lane bake executes the authoring model's
  compile step at the earliest point it exists; transforms stay declared data operations so the
  expression engine (#524) is not a dependency of the collection path.
