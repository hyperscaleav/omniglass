# Golden wire transcripts

What each simulated product put on the wire when a YAML profile defined it: the
requests of a script, answered with their statuses, headers, bytes and the
device's state after; the SNMP reads and walks with their ASN.1 types; the
sequence a drifting behavior draws from a device's seeded source; and what a
room-following device changed over the calendar's pinned room day.

They are recorded, never hand-edited. A Go family that replaces a profile is
held to them by a parity test (`sim/boreal_parity_test.go` is the worked
example): same requests, same bytes. The recorder ran once against the profile
before it was deleted (`TestRecordBorealGolden`, in this directory's first
commit); to record the next family, run the recorder against the base branch
that still holds its profile and commit the transcripts first.

The device is deterministic: loopback address 127.0.0.1, id and hostname
`wire-<product>`, clock pinned to 2026-11-03 12:00 UTC, uptime anchor an hour
earlier, behaviors held still.
