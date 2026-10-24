## frozen-fact-served-as-current (fixed; found by the #1035 review)

- **Cue:** a read that serves a latest-ever sample (a certificate's days left, a handshake's
  result) beside a gate or state that stops the probe from refreshing it.
- **Failure:** under a certificate gate the tls probe connects only, so the last accepted
  certificate's expiry and days left freeze; the panel kept showing "212 days left" in green
  beside a rejected endpoint whose component now offers another certificate entirely.
- **Fix:** tie the fact to what can refresh it: withhold it (or label it with its age) while the
  gate holds (`composeTLS`, internal/api/reachability.go), and have the surface name the refusal
  instead of reading as nothing measured.
