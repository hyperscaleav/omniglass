## stale-write-deadline (fixed; found by the #1033 review)

- **Cue:** a wrapper that writes on the read path (a negotiation reply, an ack, a keepalive answer)
  beneath a caller that sets `SetWriteDeadline` per write and never clears it.
- **Failure:** the wrapper's write inherits a long-expired deadline and fails at once, and its
  timeout surfaces from `Read`, where a reader using read deadlines as interrupts takes it for its own.
- **Fix:** clear the deadline after each bounded write, give the inner write its own deadline, and
  never return an inner write's error as the read's (internal/collection/telnet.go `Read`).
