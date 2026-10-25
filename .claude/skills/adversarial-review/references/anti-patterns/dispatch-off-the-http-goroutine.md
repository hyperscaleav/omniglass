## dispatch-off-the-http-goroutine (live)

An `http.Handler` (the API router) invoked in-process from a goroutine net/http did not start: a
protocol library's worker (an MCP tool handler on go-sdk's jsonrpc2 goroutines), a bus subscriber,
a queue consumer. net/http recovers a handler's panic per connection, so a latent panic in a route
costs one request; the same route dispatched from any other goroutine has nothing recovering it,
and the panic takes the whole process down, here the server and the bus it hosts. Found in #1162's
MCP dispatch, where any token able to call a tool could have turned a route panic into an outage.
Detection cue: `ServeHTTP(` called on a router or handler outside a net/http server (a recorder, a
synthesized request), or any handler run from a library callback. Fix: `defer recover()` at the
dispatch, mapping the panic to the 500 a direct call would get and logging the stack; pin it with a
test whose handler panics and which asserts a 500 result and that the next call still answers.
Related: the dispatched request must start on a fresh context (chi reads an inherited routing
context as a half-matched sub-route, and nothing ambient from the outer request may ride in).
