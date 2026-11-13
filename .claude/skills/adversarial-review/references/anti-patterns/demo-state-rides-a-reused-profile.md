## demo-state-rides-a-reused-profile (live)

- **Cue:** a new actor (a scale lab sender, a test fleet) reuses a simulator profile that
  ships demo state in its `defaults` (rooms, ports, users the dev lab binds or asserts on),
  and the new docs or comments claim the actor holds only what the new code gives it.
- **Failure:** the actor reports the demo state beside its own, so its traffic is not what
  the claim says (#1747: each corporate site's `ucc-platform` reported its five demo rooms
  into the conduit's unbound inbox while the ADR said every post landed on a system; #1756).
  The measure still reads 0 loss, so no test catches it.
- **Fix:** read the profile's `defaults` before claiming what the actor holds; either give
  the actor a profile or mode with no demo state, or state the leftover in the docs and file
  its removal.
