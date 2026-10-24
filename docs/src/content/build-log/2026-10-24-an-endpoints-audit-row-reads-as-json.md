---
title: "An endpoint's audit row reads as JSON and shows its whole change (#1088)"
date: 2026-10-24
refs: [1088]
---

An endpoint's audit row read its params and inputs as base64, and the long values pushed the drawer's
change table past its edge, so the After column sat off screen. The base64 came from the write: a row
image is the gateway's struct marshaled with `encoding/json`, and a jsonb column scanned raw is a byte
slice, which marshals to base64. The audit write now carries each top-level byte-slice field that holds
JSON as that JSON (`inlineJSONBytes`, through an embed like the retry's pinned endpoint), so the fix
reaches every image marshaled from a struct with such a column: an endpoint's params and inputs on
every write, and on create a driver's spec, a command type's params schema, an event type's payload
schema and a property type's validation (their updates and deletes were already JSON, read as rows).
Bytes that are not JSON, or that a jsonb column would refuse, marshal as before, and a key a tagged
field owns keeps its value. Redaction stays the write's own: an endpoint's secret inputs are references
by name. The drawer opens
in the wide panel, lays its table out fixed with each value wrapping in its own column, renders JSON
indented, and drops the column of a side with no image, so a create's spec takes the whole width.
Proven by a gateway unit test of the image (the JSON inlined under the key `encoding/json` gives the
field, a nil embed, non-JSON bytes still base64, a named embed left alone), an API test that reads an
attach, an edit and a retry back from `GET /audit-log` as objects and a driver's spec as one (each
failed first on base64), a redaction test that finds the secret's value nowhere in the trail, read as
returned or decoded from any base64 string in it, while the rows name it by reference, component tests of the drawer, and live captures of an
endpoint edit, a retry, and a driver's create and delete. Rows written before the fix keep their base64,
the record as written; decoding them on read is #1121.
