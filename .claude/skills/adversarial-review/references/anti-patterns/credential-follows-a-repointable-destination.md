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

A second form (#1370, fixed in review): another resource's secret handed out on a channel every
holder of a credential may pull (a node's subject), fenced only by an attribute a scoped writer or
the credential holder controls (an endpoint target being loopback, a self-declared capability).

- **Cue:** a new field carrying another resource's secret on a node-reachable reply, where the fence
  is computed from endpoint params rather than from a fact only the trusted lane sets.
- **Failure:** a writer who may place an endpoint (no `secret:reveal`) aims one at a loopback address
  for a component holding a webhook endpoint, and the node's credential then reads that webhook's
  token and hmac key off the simulation device list.
- **Fix:** fence on facts only the trusted lane sets (`NodeSimWebhooks`: the database's lab mark and
  the seed-allocated simulated block, internal/storage/sim_devices.go), with a test that an unmarked
  database and an out-of-block loopback component get nothing
  (`TestSimlistCarriesAWebhookSubscriptionToTheSimulatorAlone`).
