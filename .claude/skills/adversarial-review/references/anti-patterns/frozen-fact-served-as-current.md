## frozen-fact-served-as-current (fixed; found by the #1035 review, regressed and fixed in the #821 and #456 reviews)

- **Cue:** a read that serves a latest-ever sample (a certificate's days left, a handshake's
  result) beside a gate or state that stops the probe from refreshing it.
- **Failure:** under a certificate gate the tls probe connects only, so the last accepted
  certificate's expiry and days left freeze; the panel kept showing "212 days left" in green
  beside a rejected endpoint whose component now offers another certificate entirely.
- **Fix:** tie the fact to what can refresh it: withhold it (or label it with its age) while the
  gate holds (`composeTLS`, internal/api/reachability.go), and have the surface name the refusal
  instead of reading as nothing measured.
- **Second instance (#821):** a gate that stops a transport's only probe. Beneath an snmp auth
  gate the get carries the refused community, so nothing was sent and nothing landed: the verdict
  stayed up and the row read responding against a powered-off agent. Fix: give the held tick its
  own reading (the held rung lands down, rejected, `collectSNMPRung`, internal/node/snmp.go; as
  udp's gated rung does), so the verdict follows.
- **Third instance (#456):** an owner-level reduction over every instance of a key. A deleted
  endpoint's last reading is frozen (nothing prunes a series' latest row, and a changed transport
  is a delete and a new endpoint), so a worst-case or any-agrees reduction over every instance
  that ever wrote kept a component in drift, or settled a command, on what a removed connection
  once saw. Fix: reduce over live observers only, an instance that names a transport the owner
  has no endpoint of being a retired one (`latestObservedPerInstanceSQL`,
  internal/storage/instance_values.go). Age is no test on a transition-only property, where a
  live, unchanged value carries an old ts. Residual, open as #1217: an endpoint that still exists
  but whose driver no longer emits the key keeps its last reading live.
