## forwarded-header-trusted-from-any-peer (fixed for the webhook route; found by the #1024 review)

- **Cue:** `X-Forwarded-For`, `Forwarded` or `X-Real-IP` read as the client's address with no
  trusted-proxy check and no address parse, then used as a limiter key, a map key, a log field or a
  column, on a path a stranger can reach.
- **Failure:** rotating the header bypasses every per-source limit; an attacker-chosen string up
  to the header limit (about 1 MiB, invalid UTF-8 accepted) is held in memory per distinct value;
  and a stored one fails its insert (22021 invalid UTF-8, 54000 btree row size), aborting a
  whole flush.
- **Fix:** parse the address (`netip`), believe the header only from a trusted proxy network and
  take the rightmost untrusted hop, and group IPv6 by /64 (`ingress.Source`,
  internal/ingress/source.go). `clientIP` (internal/api/auth.go), which stamps a session's
  address, still reads the first hop; it is not a limiter key, and #1185 moves it onto the same
  primitive with a configured proxy list.
