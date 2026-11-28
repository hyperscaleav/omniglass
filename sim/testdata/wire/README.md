# Golden wire transcripts

What each simulated product put on the wire: the requests of a script,
answered with their statuses, headers, bytes and the device's state after; the
SNMP reads and walks with their ASN.1 types; the sequence a drifting behavior
draws from a device's seeded source; and what a room-following device changed
over the calendar's pinned room day.

They are recorded, never hand-edited. A Go family that replaces a YAML profile
is held to the transcripts the profile recorded; a family with no profile
records its own and holds itself to it. The harness is generic
(`sim/wire_golden_test.go`): a family supplies one `wireFamily` value and
nothing else. `sim/wire_family_test.go` is the smallest worked example (a test
only lamp family, proven with no other family involved), `sim/boreal_wire_test.go`
and `sim/boreal_parity_test.go` the worked example of a conversion.

## Recording a family

1. Write the family's script file, `sim/<family>_wire_test.go`: the HTTP steps,
   the SNMP OIDs and subtrees, the drifting behaviors (`tickPlan`), the room day
   (`RoomDay`), and a `wireFamily` value added to `wireFamilies` from `init`.
   Put the steps that exercise a YAML accident (a `Quirk` step) last: the
   accident leaves state behind that would carry into every step after it.
2. Record it, on the base branch while the profile still exists when the family
   replaces one:

       go test ./sim -run 'TestRecordWire$' -wire.family=<name>

   One transcript per product is written to `sim/testdata/wire/<product>.json`.
   Commit the transcripts first, then write the family.
3. Add the replay: a test that calls `replayWire(t, family, product)` for each
   product. It fails with one line per difference (status, headers, bytes, state,
   SNMP, drift, room day).

A device with more wire than HTTP and SNMP records the rest through
`wireFamily.Extra` (`sim/wire_extra_test.go`): `wireConverse` plays a
conversation on a line-protocol face (what it says on connect, each command's
answer, the lines it pushes when state moves), `wireStream` an event stream, and
`wireRecv` is a webhook receiver that records each post and whether its signature
verifies. Each is a named value in the transcript's `extra`, replayed the same way.

A family with persistent line faces (SSH, Telnet, raw TCP) or a UDP echo face adds three things to its
`wireFamily`: `Faces` records each interface it declares (protocol, port, auth, framing, welcome, limits),
`Sessions` are scripts played over a persistent face through the shell loop the three transports share (each
step sends lines back to back and may fire an HTTP request while the session listens, so a push is recorded;
subscribe to one subject at a time, since the order a change to several is pushed in is not fixed), and `Echo`
names the UDP echo faces to send a datagram to. A step's `Set` puts state paths where a scenario would have
them before the request, so a route that resets drift shows its effect.

A family replacing a profile sets `Frozen` once the profile is deleted: the
recorder then refuses it, because recording the family from itself would make
the hold on the profile vacuous.

A device with a live feed also records the named events a request or a tick published (`events`, `#events`), after its state changes, in the order the feed numbered them; a device with none records no such key.

The device is deterministic: loopback address 127.0.0.1, id and hostname
`wire-<product>`, clock pinned to 2026-11-03 12:00 UTC, uptime anchor an hour
earlier, behaviors held still.
