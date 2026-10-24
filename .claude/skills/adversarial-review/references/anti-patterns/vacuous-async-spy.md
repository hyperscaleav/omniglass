## vacuous-async-spy (live)

A synchronous `expect(spy).not.toHaveBeenCalled()` guarding the absence of a network call or
effect that is inherently a microtask away (an async middleware, a queued promise, a Solid
effect). The assertion runs before the effect could ever fire, so it passes under any
implementation, leaking or not: a pin that cannot fail pins nothing. Found in #609's
gated-fetch test, where the api client's async onRequest middleware deferred every fetch past
the synchronous assertion. Detection cue: a sync spy assertion immediately after `render()`
or `mount()` claiming an effect did NOT happen. Fix: assert the structural fact synchronously
(the query cache holds no entry for the gated key), or flush a tick before the spy assertion
and prove the test can fail by breaking the guard. The same shape after `setQueryData` (#1059):
solid-query notifies observers a few microtasks later, so one `await Promise.resolve()` asserts
before the "poll" lands and a rows-survive-a-poll test passed with the rows rebuilt; wait a
macrotask (`setTimeout(r, 20)`), assert DOM identity (`container.contains(node)`), and prove it
fails against the unkeyed iteration.
