## side-send-skips-gate (fixed; found by the #1037 review)

- **Cue:** a secondary send path (an arm or subscribe, a keepalive, a re-subscribe on recovery)
  opened beside the gated primary path, without its own check of `gates.blocked`.
- **Failure:** a udp endpoint gated for a terminal reply restarted its node: the carried-back gate
  was adopted and every poll held off, but opening the socket armed the listeners, sending the
  subscribe to the component that had refused, on every restart under the same fingerprint.
- **Fix:** every send path checks the gate before it sends, as a held session starts rejected and
  dials nothing (`armDatagram`, internal/node/datagram.go;
  `TestAUDPEndpointArmsNothingBeneathAGate`).
