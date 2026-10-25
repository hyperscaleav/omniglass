## auth-fails-open-on-missing-credential (fixed; found building #1024)

- **Cue:** an inbound check compares what a sender presents against a credential resolved at
  request time (an hmac key, a shared token, a basic login unsealed from a secret), and the
  resolution can come back empty or absent (the secret deleted, renamed, or holding a blank
  field) without the check refusing first.
- **Failure:** the comparison runs against the empty string, which any sender can present: a
  basic login whose password is blank admits `user:`, an hmac over an empty key is computable by
  anyone, a blank token matches an empty header. The endpoint reads authenticated while it
  checks nothing.
- **Fix:** fail closed twice. The host refuses before comparing when the scheme is credentialed
  and its credential did not resolve (`CredentialMissing` in the webhook receiver,
  internal/api/webhooks.go), counted under its own reason; and the check itself refuses any
  empty field for every scheme but an explicit none (`ingress.Authenticate`,
  internal/ingress/webhook.go), so no caller that forgets the first guard can open it.
