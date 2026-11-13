## window-goroutines-outlive-early-return (fixed; found by the #1632 review)

A function starts workers for a bounded span (a measurement window, a batch, a request's fan-out)
and collects them with a wait func it calls only on the success path. Any early `return err` between
the start and the wait leaves the workers running on a context nobody cancels; when the caller loops
(a sampler, a retry), the leftovers overlap the next span and skew or overload it. Found in #1632's
`lab measure --every`, where a failed fleet view read left the system page clients loading into the
next sample's window, on a context made uncancellable so a stop finishes the sample in flight.
Detection cue: a `go func` or a `wait := start(...)` followed by more than one `return` before the
wait runs, on a context the function did not derive itself. Fix: derive a child context for the
workers and `defer` its cancel plus the wait, so every path stops and joins them; pin it with a test
that fails the span partway and asserts no worker effect arrives after return
(`internal/labfootprint/page_load_test.go`, `TestLoadWindowStopsThePageClientsOnAFailedRead`).
