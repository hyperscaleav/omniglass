## fixture-binds-a-fixed-port (fixed; found by the #1061 review; the simulator fixed by #1066, the e2e servers' pick-and-release ports by #1077; watch for regressions)

- **Cue:** a test or fixture that serves on a literal address and port (a simulated component's face
  at `127.x.y.z:20023`, a `StartTCPSession(51325)`, a port lifted by a fixed offset); a "fresh"
  address from a package-level counter (it isolates a `-count` rerun, never a second process); a
  pick-and-release free port handed to a server that binds it later.
- **Failure:** two test processes on one host (two worktrees, a session beside `make test`) bind the
  same address and one fails `bind: address already in use`, or worse, a client in one run reaches
  the other run's server and asserts against the wrong state. The suite is flaky only under the
  parallel use it most needs to survive.
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
  The testcontainer form of the class is `fixed-testcontainer-port`.
