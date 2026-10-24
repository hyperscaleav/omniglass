---
title: "A driver-attached endpoint's connection edits in place (#1061)"
date: 2026-10-23
refs: [1061]
---

The architect's ruling before merge: re-attach is not the path. `PATCH /endpoints/{id}` takes a
driver-attached endpoint's `inputs` (the host, the port, an attached secret's reference), merged over
the stored ones and checked against the driver's declared inputs (a non-number port, an undeclared
input, a raw `params.target`, or a transport the driver does not speak is a 400 naming the field),
every attached secret re-resolved under the editor's own secret scope (an editor cannot redirect a
credential they could not use), audited, and the tasks re-derived in the same transaction through the
one per-endpoint derivation #823's driver update now shares (`rederiveEndpoint`; ids move only where
the derived spec does). The console's endpoint blade edits the declared inputs in place, sending only
the ones changed, and `omniglass endpoint update <id> --inputs` regenerates. Proven against Postgres
(the port and address and secret edits, task ids unchanged, the refusals, two audit rows), over the
API (a 400 naming `port`, the viewer refused), in vitest (only changed inputs sent, a label-only edit
sends no inputs), and end to end (`TestADriverEndpointEditsItsAddressInPlace`: a wrong port reads
down with no login; the port edited in place connects and authenticates within a tick; a label edit
recycles nothing; the address edited in place moves the healthy session to the other component).
Recorded in ADR-0158 point 6.

The in-place edit's review, each with a test that failed first: a `params.timeout` value is parsed
and refused by name unless it is a positive duration; an input a driver update dropped no longer
blocks an edit; an empty value clears an optional input (its default fills it again) and refuses a
required one by name, the console sending a cleared field rather than dropping it; the row is locked
before an edit merges over it, so two concurrent edits cannot lose one; a transport restatement is
judged against the endpoint's own. End to end, an in-place swap of the attached secret now un-gates
a session its stale credential had rejected, within a tick.

CI on the rollup found two more. A telnet test's scripted peer hung up as soon as it reported, racing
the test's peek at the connection; the peer now stays connected and the test proves the connection
survived with a second exchange (a review catalog entry records the pattern). And a stateless poll
kept the failure backoff it built up against an address since edited in place, so its first good
fetch could wait tens of seconds: a poll whose endpoint's connection fingerprint moves now runs at
the next tick. The `fix-an-endpoint-in-place` walkthrough ships on the operator collection guide.
