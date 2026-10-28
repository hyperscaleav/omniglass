## credential-follows-a-repointable-destination (fixed)

- **Cue:** a stored binding of a credential to a destination (a notification target's secret and
  URL, an integration's token and endpoint) where creating or rebinding checks the caller's right
  to the credential, but an edit to the destination alone does not.
- **Failure:** a holder of the destination's write permission who may not read the credential
  repoints the destination to a URL it controls, then triggers a send (`:test`, or the next
  delivery): the platform hands it the credential, a confused deputy. Proven in the #1320 review:
  a `notification_target:*`-only role received an admin-sensitive bearer token this way.
- **Fix:** every path that changes where or how a bound credential is delivered takes the right
  to reveal it (`resolveTargetSecret` under the caller's secret read and reveal scopes and admin
  tier, internal/storage/notification_targets.go), with a test that a URL-only patch by a caller
  without that right is refused and the same patch by one with it succeeds.
