## forwarded-header-trusted-from-any-peer (fixed; found by the #1024 review, closed by #1185)

- **Cue:** `X-Forwarded-For`, `Forwarded` or `X-Real-IP` read as the client's address with no
  trusted-proxy check and no address parse, then used as a limiter key, a map key, a log field or a
  column, on a path a stranger can reach.
- **Failure:** rotating the header bypasses every per-source limit; an attacker-chosen string up
  to the header limit (about 1 MiB, invalid UTF-8 accepted) is held in memory per distinct value;
  and a stored one fails its insert (22021 invalid UTF-8, 54000 btree row size), aborting a
  whole flush.
- **Fix:** read the client through the one primitive, `clientaddr.Trust.Client`
  (internal/clientaddr/clientaddr.go): it parses the address (`netip`), believes the header only
  from a peer in `OMNIGLASS_TRUSTED_PROXIES`, and takes the rightmost untrusted hop. Both the
  credential stamp (`clientIP`, internal/api/auth.go) and the webhook source (`ingress.Source`,
  which adds the IPv6 /64 grouping a limiter key needs) read it; a new reader of a forwarded header
  anywhere else is this pattern again.
