## script-owned-signal-kills-the-child (fixed; found by the #1197 review)

- **Cue:** a supervisor script owns a child's teardown through one signal from its trap (a
  background `&` child it stops with SIGTERM), and the child keeps the default disposition
  for another signal that reaches it: SIGINT or SIGHUP from the terminal, which go to the
  whole foreground process group, or SIGPIPE when a reader of its output (a prefixer, a
  `tee`) dies first.
- **Failure:** the child dies of the other signal before its trap's SIGTERM arrives, so its
  teardown never runs, while the script's trap still reports a clean teardown. Found on
  #1197: `make walkthroughs` in a closed terminal, or `make demo | tee log` and Ctrl-C, left
  the lab's containers and volumes behind.
- **Fix:** the child ignores every signal the script owns (INT, HUP, PIPE) when it is held
  for a script, and stops only on the one signal the trap sends; the script reports a
  teardown only when it stopped a live child (exemplar: `internal/cli/lab.go` with
  `--while-pid`, `web/e2e/lib/lab.sh`).
