## rawmessage-carries-non-json (fixed; found by the #902 review)

- **Cue:** a `json.RawMessage` field assigned bytes that are not JSON (an encoded form body, an xml
  or text document), or a wire struct whose field type promises JSON while a render step fills it
  with its final encoding.
- **Failure:** `json.Marshal` of the enclosing message fails ("invalid character ... looking for
  beginning of value"), so a reply that carries it is never sent; when the pull already stamped its
  rows (a command queue marked dispatched), one bad row blocks every row beside it until its window
  passes, with no error recorded on any of them.
- **Fix:** keep the field JSON end to end and encode at the edge that sends it
  (`driver.Request.BodyBytes`, called by the http fetcher), and prove it with a marshal round trip
  per format.
