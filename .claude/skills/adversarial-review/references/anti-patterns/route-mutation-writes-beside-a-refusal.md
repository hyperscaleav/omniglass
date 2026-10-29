## route-mutation-writes-beside-a-refusal (fixed in its known profile; found building #895)

- **Cue:** a simulator route with two or more mutations derived from the same request fields,
  where only some carry the enum or the guard that refuses a bad value.
- **Failure:** `applyMutations` applies each mutation on its own, so on a request answered 400 the
  unguarded ones still write, and the refusal path returns before the version bump and the diff
  publish: the device holds a value nothing published (a `<no value>` signal for an unknown input,
  a new input key a typo created) until a behavior repairs it, and a driver reading it faults.
- **Fix:** guard every mutation path on every request field it reads (`{{ guard "<pattern>"
  .Request.Body.<field> }}` in the path), and assert in the refused case that each path the route
  writes held still (`meridian-mx-84`'s `/api/route` and `/api/inputs/signal`).
