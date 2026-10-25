## peer-id-dedup-outlives-peer (fixed)

A dedup set keyed on ids the peer assigns (event ids, message ids, sequence numbers) that outlives
the peer's id space: it survives reconnects and recycles while the peer reboots and counts from 1
again, is replaced by another unit at the same endpoint, or reuses ids (timestamps to the second).
Every new event whose id was seen before is silently dropped, which is data loss dressed as
idempotence. Found in #1038's first resume design, which kept the last 1024 event ids across a
stream's reconnects. Detection cue: a long-lived set of peer-assigned ids consulted before
delivering, with no tie to the connection or the peer's identity. Fix: dedup only what the protocol
says can repeat (a stream's resume replays at most the one event it resumed from, as the
connection's first event), keyed to the connection, and reset any resume when the endpoint's
address changes.
