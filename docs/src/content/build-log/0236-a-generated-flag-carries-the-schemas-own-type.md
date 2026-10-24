---
title: "A generated flag carries the schema's own type"
date: 2026-08-14
refs: []
form: bullet
---

- **A generated flag carries the schema's own type**
  ([#711](https://github.com/hyperscaleav/omniglass/issues/711)). Every body flag the CLI generator
  emitted was a string, and every non-string field was coerced at run time by `jsonOrString`. So the
  spec said `integer`, the flag said `string`, the generated CLI reference published `string`, and an
  operator who typed a word learned about it from the server's 422 after a round trip that had already
  authenticated. That is the generate-first drift class the repo's own rule names, sitting inside the
  generator: a fact the spec states, restated by hand as something else.

  `cmd/cligen` now maps the property's type to the flag that carries it, so `--settle-window-seconds
  soon` is refused by the shell before a request is issued, `--ttl-days` is an `int` and
  `--sensitive` a `bool` in `--help` and in the reference. A structured field keeps ONE string flag
  parsed as JSON, and that is a ruling rather than a leftover
  ([ADR-0112](/architecture/decisions/#adr-0112-a-generated-flag-carries-the-schemas-type-and-a-structured-field-carries-json)):
  a nested value has no shell-native flag type, and `null` has to stay sendable because it is what
  clears a field named in `update_mask`. A nullable STRING is the single exception, since this API
  clears a string with the empty string.

  The tests drive the REAL generated tree against a canned server rather than the generator's model:
  a word for an integer flag is refused with the server as a tripwire that fails the test if a request
  was issued at all, `--settle-window-seconds 15` arrives on the wire as the JSON number 15, and both
  spellings of the object case are pinned (`--name-rule '{...}'` and the `--name-rule null` that
  clears it under the mask), so a later move to per-leaf flags cannot quietly take the second away.

  One documented line broke, and the guard that found it is the point. A bool flag reads
  `--propagates false` as a positional argument, so `--required true` in the CLI guide could no longer
  work; the docs flag check now fails on a bool flag handed a space-separated `true` or `false`. Two
  expectations moved in `cmd/cligen/main_test.go`, both on the boolean: `propagates.JSON` was `true`
  and is `false`, and the rendered body line was `body["propagates"] = jsonOrString(fPropagates)` and
  is `body["propagates"] = fPropagates`.

  **The issue's own example was wrong and is corrected here.** It names `expected_ordinal` as one of
  the two fields that surfaced this; the #702 review replaced that field with `expected_name` and
  `internal/docslint` refuses the word, so there is no integer field by that name to cover.
  `settle_window_seconds` is the integer under test in its place.
