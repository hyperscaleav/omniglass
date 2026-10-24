---
title: "The acronym list is one replaceable setting, not a shipped list plus operator additions"
date: 2026-08-10
status: "Accepted"
summary: "The acronym dictionary `title` consults is ONE key, `label.acronyms`, in a new `platform,client` settings namespace; an operator's list REPLACES the shipped one and provenance tells them apart, rather than a union of shipped plus additions (which would give one key a merge rule no other setting has, make the wire value a fragment rather than the effective dictionary, and make a shipped entry unremovable). The engine resolves the dictionary at render time and caches the compiled engine against the dictionary ITSELF, so a change builds a replacement rather than mutating one and no writer has a generation counter to forget; validation uses a dictionary-less engine, since whether a rule parses is a fact about the rule alone"
---

- **Date:** 2026-08-10 | **Status:** Accepted | **Pages:** [settings](/architecture/settings/),
  [core entities](/architecture/core-entities/), [glossary](/architecture/glossary/)
- **Decision:** The acronym dictionary a label rule's `title` consults is **one key**,
  `label.acronyms`, in a new `platform,client` settings namespace. An operator's list **replaces**
  the shipped one; the two are told apart by the settings engine's existing **provenance**
  (`sources["label.acronyms"]` reads `default` or `platform`), not by keeping them as separate
  values and unioning them at read time.
- **Why not shipped-plus-additions:** The epic's scope text reads as a union, and a union is what an
  operator adding one word wants. It was rejected because of what it costs everywhere else. Merge in
  this engine is presence-based over generic maps and a non-map value overrides wholesale, so a
  union would be a merge rule this one key does not share with any other setting, and reading
  `label.acronyms` would no longer tell you the effective dictionary: the value would be a fragment
  and the engine would hold a list nothing on the wire reports. It also makes a shipped entry
  **unremovable**, which matters more than it sounds, since a word we ship that an operator's estate
  spells differently would then be uncorrectable. The union's one benefit, not retyping the list to
  add a word, is a console problem, and the console already read-modify-writes the effective value
  for every other setting.
- **Consequence, stated rather than discovered:** an operator who overrides the list stops receiving
  later releases' additions to it, exactly as overriding any other setting freezes it. The console
  badges the key as overridden, and restoring the namespace returns the shipped list.
- **Decision (the engine's lifecycle):** The dictionary is resolved from the settings cascade **at
  render time**, and the compiled engine is cached against the dictionary itself
  (`label.EngineCache`). Parsing binds a template's FuncMap, so a dictionary change produces a
  **new** engine rather than mutating one, and a rule compiled against the old engine keeps the old
  casing for as long as it lives; the storage path re-parses on every render, so the longest a stale
  dictionary survives is one render and an operator's edit needs no restart. A generation counter
  bumped by the settings write path was rejected for having a failure mode a content key cannot
  have: a second write path that forgets to bump it.
- **Consequence:** a render costs one read of the override table, on a write path already several
  round trips deep. A caller rendering many rows (the bulk recompute, [#685](https://github.com/hyperscaleav/omniglass/issues/685))
  resolves the engine once and passes it down, which is why the render functions take an engine
  rather than reaching for one.
- **Decision (the gateway reads that override in the caller's transaction, not off the pool):** A
  stamp runs inside a transaction, so it is already holding a connection; issuing the settings read
  against the pool acquires a second one, and a pool whose connections are all held by writers each
  waiting for a second connection is **deadlocked**, not slow. It needs no unusual code to reach (a
  bulk import of the estate sizes this epic exists for is enough) and it fails as a hang. So
  `Service.ResolveOverride` takes an override level the caller has already read, over the same level
  stack `Resolve` builds, and the gateway reads it on its own transaction; the dictionary and the row
  being stamped then come from one snapshot as well. A one-connection pool is the whole population of
  that race, which is how the regression test reaches it deterministically rather than under load.
- **Decision (parse and render need different engines):** Rule **validation** uses a
  dictionary-less engine, because `label.New` binds the same four function names whatever dictionary
  it is given and the grammar check walks a static allowlist: whether a rule parses is a fact about
  the rule alone. Only execution differs, so only the render path carries the current engine, and a
  template compiled by the validator is discarded rather than reused.
- **Tracked under** [#684](https://github.com/hyperscaleav/omniglass/issues/684), the fourth slice of
  epic [#657](https://github.com/hyperscaleav/omniglass/issues/657).
