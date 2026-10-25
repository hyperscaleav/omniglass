## deadline-cleared-before-body-read (fixed)

An auth step (or any staged connect) sets a read deadline for a request's answer head, clears it
once the head is in, and then reads the answer's body on the "not what we wanted" path with no
deadline and nothing watching the context. A peer that answers with a head and then holds the body
open (a feed served under the wrong media type, a refusal whose body stalls) pins the step forever:
the session reads establishing for good, whatever waits on its verdict waits with it, and closing
the session blocks on the machine, so a teardown hangs the caller's loop. Found in #1038's stream
dialer. Detection cue: `SetDeadline(time.Time{})` (or a cleared timer) between reading a head and
reading a body, or an auth step with no `context.AfterFunc` that cuts the connection on cancel.
Fix: hold the deadline from the request through any refusal's body, clear it only on the path that
hands the connection to its long-lived reader, and hook the context to cut the connection.
