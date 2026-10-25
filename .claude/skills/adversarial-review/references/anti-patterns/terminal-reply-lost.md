## terminal-reply-lost (fixed; found by the #1049 review, regressed and fixed again in #1034)

- **Cue:** a session-wide signal channel that an exchange goroutine feeds after the connection's
  `Done` can already have fired, and that the next connect drains as stale. A second shape (the
  #1034 websocket): a reader that hands messages to a router goroutine over a channel and closes
  `Done` itself, while the router and the exchange select on `Done`.
- **Failure:** a component answers with a reply the driver declares terminal and hangs up; the
  machine sees the drop first, books an ordinary failure, discards the late signal on the next
  connect, and redials into the lockout. In the router shape the answer, terminal reply, ack or
  last push read just before a close is dropped, measured at 90 losses in 200 against an idle
  router.
- **Fix:** the signal belongs to the connection (allocated with it, `lend` in
  internal/collection/session.go), and after `Done` the machine waits for the connection's
  in-flight exchanges before deciding between fail and reject. In the router shape the reader's
  end travels in order through the same channel and only the router fails the connection
  (internal/collection/websocket.go), and an exchange prefers a ready answer over `Done`
  (`TestWebSocketRoutesWhatCameBeforeTheClose`, `TestWebSocketTerminalReplyBeforeAHangUpRejects`).
