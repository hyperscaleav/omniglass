## fixture-binds-a-fixed-port (fixed; found by the #1061 review; the simulator fixed by #1066, the e2e servers' pick-and-release ports by #1077, the refused dials' by #1089; watch for regressions)

- **Cue:** a test or fixture that serves on a literal address and port (a simulated component's face
  at `127.x.y.z:20023`, a `StartTCPSession(51325)`, a port lifted by a fixed offset); a "fresh"
  address from a package-level counter (it isolates a `-count` rerun, never a second process); a
  pick-and-release free port handed to a server that binds it later; a "closed" port found the same
  way (listen on `:0`, read the address, close) for a dial the test expects refused.
- **Failure:** two test processes on one host (two worktrees, a session beside `make test`) bind the
  same address and one fails `bind: address already in use`, or worse, a client in one run reaches
  the other run's server and asserts against the wrong state. A released port is free the moment
  its listener closes, so a dial expected refused is answered by whoever was handed the port next
  (beside a churning squatter, about 3% of the collection package's refused-dial runs connected).
  The suite is flaky only under the parallel use it most needs to survive.
- **Fix:** bind port 0 and read the address back: an in-process simulator serves on
  `sim.EphemeralPorts` and tests aim at `Device.Faces`; a face restarted mid-test rebinds the port
  it reported. Where a port is fixed by design (the `sim run` mode binds the profile's ports on the
  endpoint's address, so the endpoints must target those) or pinned on purpose (a port bound on one
  address and rebound on another), claim an address no other test holds (`simtest.ClaimLoopback`).
  An out-of-process server binds port 0 too and reports what it got: `omniglass server` takes
  port 0 on `OMNIGLASS_ADDR` and `OMNIGLASS_NATS_ADDR` and writes the bound addresses to
  `OMNIGLASS_READY_FILE`, and the e2e tests start it only through `startServer`, which a guard
  test (`TestOnlyStartServerStartsAServer`) holds them to. Mind a library that reads port 0 as
  its own default: nats-server binds 4222 for 0, so the bus maps 0 to its ephemeral `-1`.
  A test that needs a refused dial aims at `nettest.RefusedAddr`: a socket bound to a loopback port
  and never listening holds it, so the connect is reset and nothing else on the host can listen
  where it lands (a UDP refusal aims at a privileged port, which `bind(:0)` never hands out). The
  guard `TestNoTestUsesAReleasedPort` (`internal/build`) fails any test that uses a listener's
  address after closing it, with a short list of excused uses (a listen port the code under test
  must be told, picked again on a collision). The testcontainer form of the class is
  `fixed-testcontainer-port`.
