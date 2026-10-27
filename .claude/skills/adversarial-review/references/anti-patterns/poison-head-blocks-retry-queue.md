## poison-head-blocks-retry-queue (fixed; found by the #453 review)

- **Cue:** an ordered retry queue that holds its head on every publish error and ships
  nothing behind it until the head succeeds, with no split between an error a retry can
  change and one it cannot.
- **Failure:** one message the bus can never take (over the server's max payload, a
  malformed subject) fails client-side on every attempt, so the node's spool delivered
  nothing behind it until enough new batches arrived to overflow the cap and drop it,
  hours on a typical node, with endpoint freshness unstamped all the while. Before the
  spool, the same batch failed once and the rest kept flowing.
- **Fix:** classify the errors: a permanent one drops the message and tells it, counted as
  a drop (`refused`, internal/node/spool.go), as the consumer splits `Term` from `Nak`;
  only a transient one holds the queue.
