## serial-ack-on-caller-goroutine (fixed; found by the #453 review)

- **Cue:** a publish path that waits a full ack round trip per message, on whichever
  goroutine called send, so the caller ships everyone's queued messages before it returns.
- **Failure:** the node's spool shipped at most one batch per round trip (about 20 a second
  at a 50 ms WAN), below a few chatty listeners' rate, so the queue grew until overflow
  dropped data; a session's router goroutine that happened to send was held for seconds
  shipping others' batches, timing out the exchanges waiting on it; and a full spool's
  catch-up on the tick goroutine stalled heartbeats, polls and run-now jobs for minutes.
- **Fix:** a send only queues; one shipper goroutine owns publishing and keeps a bounded
  window with the stream, in order, removing heads in ack order and re-sending from the
  first unacked under the same ids on a failure (`ship`, internal/node/spool.go). Order
  holds only if nothing is buffered across a reconnect (`nats.ReconnectBufSize(-1)`), since
  one connection delivers in publish order.
