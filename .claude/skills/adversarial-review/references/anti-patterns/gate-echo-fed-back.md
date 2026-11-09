## gate-echo-fed-back (fixed)

- **Cue:** a gate (or circuit breaker, or backoff) that both suppresses a probe and is fed by that
  probe's result: the suppressed path returns a synthetic failure reading, and a second reason or
  counter downstream treats that reading as an observation.
- **Failure:** the gate's own output closes a second gate. Found in the #1650 review: a refused snmp
  community (or a udp terminal reply) makes the rung read down, rejected, without sending anything;
  the probe verdict read that as down, so two probes later the endpoint also held `unreachable`. An
  operator's retry lifted the refusal but polls, run now and commands stayed held behind
  `unreachable` until a probe read up.
- **Fix:** mark the synthetic reading (here the `rejected: ` reason the gated rungs carry) and treat
  it as no verdict, as an inconclusive probe is (`probeVerdict`), with a run-loop test that drives the
  real rung under a refusal and asserts no second reason is added.
