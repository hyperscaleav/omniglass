## side-run-skips-change-memory (live)

A second path that runs the same unit of work as a scheduled loop (an on-demand run, a retry, a
manual trigger) and publishes its result directly, skipping the state the loop keeps between runs:
the #1145 change memory that holds an event or a log line until it changes, a transition-only
verdict, a fault-seen set. The side path lands a duplicate the loop would have held, and leaves the
memory stale, so the loop's next run lands the same thing again. Found in #1163's run now, which
published its poll's samples without `sched.changed` and `sched.commitChanged`, so every run now
could land an event twice and fire its rules twice. Detection cue: a function that calls the same
collector the loop calls, then publishes, without touching the loop's per-task memory. Fix: route
the side path's landing through the loop's own filter and commit (here `landableRun`), and pin it
with a test that runs the side path twice over an unchanged event and the loop after it.
