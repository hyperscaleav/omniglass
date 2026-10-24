## byte-truncated-text-into-proto-string (fixed; found by the #902 review)

- **Cue:** text from outside (a component's error page, a `Location` header, a peer's reason) cut
  with `s[:n]` or passed through unchecked into an error, a reason label or an event message that a
  protobuf message carries.
- **Failure:** a multibyte character split by the cut, or a body in Latin-1, leaves invalid UTF-8;
  `proto.Marshal` refuses it ("string field contains invalid UTF-8"), the batch or the
  collection-failed event is not published, and an error returned from the publish ends the tick
  (or the node run, on the first tick) and repeats on every restart.
- **Fix:** cut on a rune boundary after `strings.ToValidUTF8`, and sanitize where the message is
  built (`validLabels` and `publishCollectionFailed`, internal/node/probe.go); remember a fault as
  told only once it published.
