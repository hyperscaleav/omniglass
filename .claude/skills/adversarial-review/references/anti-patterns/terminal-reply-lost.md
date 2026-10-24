## terminal-reply-lost (fixed; found by the #1049 review)

- **Cue:** a session-wide signal channel that an exchange goroutine feeds after the connection's
  `Done` can already have fired, and that the next connect drains as stale.
- **Failure:** a component answers with a reply the driver declares terminal and hangs up; the
  machine sees the drop first, books an ordinary failure, discards the late signal on the next
  connect, and redials into the lockout.
- **Fix:** the signal belongs to the connection (allocated with it, `lend` in
  internal/collection/session.go), and after `Done` the machine waits for the connection's
  in-flight exchanges before deciding between fail and reject.
