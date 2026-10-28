## platform-stamp-without-check (fixed; found by the #417 review)

- **Cue:** a route registered with `a.platformGated(...)` (or `conditionalGated`) whose handler
  calls none of `requirePlatform`, `canPlatform` or `allows`.
- **Failure:** `platformGated` only stamps `x-omniglass-platform-permission` on the operation for
  the spec and the console; it enforces nothing. The spec, the CLI help and the docs all claim the
  second permission, while any caller holding the primary one passes. The first `setRetention`
  routes let the seeded Operator role (a type's update permission, no `platform:*`) set a shipped
  type's retention to one day, which the next pass would act on.
- **Fix:** enforce in the handler (`if err := a.requirePlatform(ctx, "update"); err != nil`),
  test a caller who holds the primary permission but not the platform one, and keep the class
  closed with the source guard `TestEveryPlatformStampedRouteEnforcesIt`
  (`internal/api/platform_stamp_guard_test.go`).
