## fixture-hangs-up-before-the-check (fixed; found by CI on #1061)

- **Cue:** a scripted peer in a real-wire test that reports over a channel and then returns, its
  deferred `Close` racing a non-blocking check on the client's `Done()` or state.
- **Failure:** under scheduler pressure the client sees the peer's EOF before the test looks, and
  a correct EOF fails the test intermittently on CI while passing locally (`telnet_test.go`, CI run
  40016892001). A 50ms pause before the check reproduces it deterministically.
- **Fix:** hold the fixture open until the test ends (a `release` channel closed on return), and
  prove liveness with an action over the connection (a second exchange answered), never a peek.
