## reconnect-budget-under-a-retry-buffer (fixed; found by the #453 review)

- **Cue:** a buffer that retries "on reconnect" sitting on a NATS connection opened with the
  client's defaults (60 reconnect attempts, 2 s apart), with no closed handler.
- **Failure:** an outage longer than the budget (a few minutes: a server upgrade running
  migrations, a WAN link down) closes the connection for good; every retry fails at once
  with the connection closed, the spool fills and drops, and the node keeps running and
  never ships again until a restart loses what it held. The docs' "buffers in memory and
  replays on reconnect" was false past the budget.
- **Fix:** reconnect without limit on any connection a retry buffer depends on
  (`nats.MaxReconnects(-1)`, `connectBus`, internal/node/run.go, as internal/simnode does),
  and bound the buffer's age below whatever deduplicates its retries downstream (the spool's
  12 hours inside the receipt's 24).
