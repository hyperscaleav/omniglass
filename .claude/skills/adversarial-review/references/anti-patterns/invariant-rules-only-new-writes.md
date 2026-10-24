## invariant-rules-only-new-writes (fixed; found building #1100)

- **Cue:** a write-side invariant (a refusal at the gateway, a declaration-point check) shipped over
  tables and stored declarations that already hold rows written without it, with nothing re-reading
  those rows against it.
- **Failure:** the invariant rules only what is written next. A row an earlier release let any writer
  land keeps acting where it is read (a control read still takes it, a forward-dated one stays
  latest), a stored declaration keeps compiling into derived work, and a carried in-memory copy of
  server-delivered state outlives the server withdrawing it.
- **Fix:** hold legacy data where it acts, structurally: mark the provenance the invariant trusts on
  the row and read by the mark (recovering history from an existing audit trail), retire rows the
  invariant refuses into a kept table rather than deleting or flagging them, re-check stored
  declarations where they are delivered and audit them at boot, and make the holder of delivered
  state drop what is no longer delivered (ADR-0163; `keyPin`, internal/storage/endpoints.go;
  `rulingEmits`, internal/storage/legacy_writer.go; `HostKeyPin.Learned`, internal/collection/session.go).
