## scrub-after-transform (fixed; found by the #902 review)

- **Cue:** a credential scrub (`Scrub`, a redaction pass) applied to text after something has
  already collapsed whitespace, truncated it, or re-encoded it.
- **Failure:** the transform changes the credential's form before the scrub looks for it: a cut
  through the secret leaves its prefix (half a bearer token, the start of a basic header's base64,
  which decodes to the start of `user:password`), and a collapsed double space no longer matches the
  password; the remainder lands in an error, an event, a log line or a stored `exec_error`.
- **Fix:** scrub the whole raw text first, every encoded form of each secret (raw, url-, JSON- with
  and without an escaped slash, html-escaped), then collapse and cut (`snippet`,
  internal/collection/httpfetch.go), with a test whose echo straddles the cut.
- **Second instance (#1320 review, fixed in the same slice):** the egress client copied a
  receiver's error body into the reason a `:test` audit row keeps with no scrub at all, so a
  receiver echoing its request path landed a chat webhook URL's credential path there. Fixed the
  same way: `egress.Post` reads past the cut, scrubs the URL, its path and query and every
  credential header in each encoded form, then cuts (`TestAReceiverEchoNeverCarriesTheURLOrTheToken`).
