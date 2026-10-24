---
title: "A generated flag carries the schema's type, and a structured field carries JSON"
date: 2026-08-13
status: "Accepted"
summary: "`cmd/cligen` derives each body flag's TYPE from the OpenAPI property: an `integer` field is an `int` flag, a `boolean` a `bool` flag, a `number` a `float64` flag, so a value the schema refuses is refused at the shell rather than by the server's 422. Every other shape keeps ONE string flag parsed as JSON (an object, an array, an untyped `any`, and a nullable number or boolean), because a nested value has no shell-native flag type and `null` has to stay sendable: it is what clears a field named in `update_mask` (ADR-0106). A nullable STRING is the exception and stays a plain string flag, since this API clears a string with the empty string. `--propagates=false` becomes the spelling for a bool flag, and the docs flag check fails on a bool flag handed a space-separated value"
---

- **Date:** 2026-08-13 | **Status:** Accepted | **Pages:** [the CLI](/guides/cli/), [API first](/contributing/api-first/)
- **Decision:** `cmd/cligen` derives each body flag's **type** from the OpenAPI property it comes
  from. A scalar the shell can type takes that flag type (`integer` is an `int` flag, `boolean` a
  `bool` flag, `number` a `float64` flag, `string` a string flag), so a value the schema refuses is
  refused **at the shell** rather than by the server's 422 after an authenticated round trip. Every
  other shape keeps **one string flag parsed as JSON**: an object, an array, an untyped `any`, and a
  nullable number or boolean.
- **Context:** every body flag was a string, and every non-string field was coerced at run time by
  `jsonOrString`. The schema said integer, the CLI said string, and the generated CLI reference
  published `string` for all of them, which is the generate-first drift class moved inside the
  generator: a fact the spec states, restated by hand as something else. The same wave surfaced it
  twice, on `name_rule` (an object) and on `settle_window_seconds` (an integer with a floor of 0 the
  flag could not carry).
- **Why a structured field stays one JSON string, decided rather than left:** a nested value has no
  shell-native flag type. The alternatives are repeated `key=value` pairs (pflag's `StringToString`),
  which can express neither nesting nor an array member, or a flag per leaf, which would make the flag
  NAMES depend on how a `$ref` happens to nest and rename them when it changes. Above both,
  **`null` has to stay sendable**: a nullable object is cleared by naming it in `update_mask` and
  sending null ([ADR-0106](#adr-0106-a-location-type-is-platform-owned-and-a-nullable-object-clears-under-the-mask)),
  and a typed flag has no null, so `--name-rule null` is the only spelling of that clear. That is why
  a nullable number or boolean keeps the JSON spelling too, though the spec carries none today.
- **The one nullable exception:** a nullable **string** stays a plain string flag. This API clears a
  string with the empty string rather than with null, and routing it through JSON would hand it the
  quoting hazard the string passthrough exists to avoid (a name that is literally `30`).
- **What moved for an operator:** a boolean is now a real bool flag, so `--propagates=false` is the
  spelling and `--propagates false` reads `false` as a positional argument. One documented line in the
  CLI guide was exactly that mistake, so the docs flag check now fails on a bool flag handed a
  space-separated `true` or `false`, which is a claim a page can no longer make by accident. A
  required flag's rendered example names what the flag takes (`--position <int>`, `--fields <json>`)
  instead of repeating the field name, since `--position position` read as a runnable line and is now
  refused by the parser rather than by the server.
- **Corrected on the way in:** the issue names `expected_ordinal` as one of the two fields that
  surfaced this. That field does not exist: the #702 review replaced it with `expected_name`, and
  `internal/docslint` refuses the word. `settle_window_seconds` is the integer under test instead.
- **Tracked under** [#711](https://github.com/hyperscaleav/omniglass/issues/711).
