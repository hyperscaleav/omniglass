---
title: "The demo pipeline rides the lab (#1197)"
date: 2026-10-27
refs: [1197, 1193, 1267, 1268]
---

The last slice of the lab epic. The walkthroughs, the terminal demos and the probe record on the
product's own lab, `omniglass lab run`, instead of the walkthrough stack, a bash recipe that built the
same topology by hand; `stack_walkthrough_up`, `make walkthrough-stack`, `make dev-sim-container` and
`make test-walkthrough-stack` are gone, and `web/e2e/lib/stack.sh` keeps only the Postgres-and-server
half the browser e2e tier and the docs screenshots use (#1267 moves those too). A recording now shows
what a learner's lab shows, the "lab instance" banner included, and all thirteen walkthroughs were
re-recorded on it once, each within its budget.

Each recorder runs `./bin/omniglass lab run` as a throwaway lab (`web/e2e/lib/lab.sh`): a project of
its own per run, so two recordings on one Docker daemon never meet, on a port Docker picks, seeded with
the dev fleet at the 2s cadence. Three hidden flags make that a script's lab rather than a learner's:
`--ephemeral` removes its volumes at teardown (and lets the next run reap an abandoned one of any
project), `--ready-file` writes, once ready, how to reach it from a container on its network, with an
owner token minted for the run, and `--while-pid` ends the hold once the script's process is gone. The
script's trap stops the lab with one SIGTERM on every exit path; a recorder killed outright leaves the
lab to notice and tear itself down within two seconds, and a lab disturbed from outside fails the run
saying so. A helper container (Playwright, ffmpeg, VHS) carries the lab's labels, so the lab's own
teardown removes it too. `make walkthroughs`, `make demo` and `make probe` now build first, and the
run:demo workflow runs `make` before it records.

The one component the stack added for the set-up-monitoring walkthrough, `bay-2-display`, is that
walkthrough's own `setup` now: a new optional list of API creates the recorder (and the probe) makes
off camera before the title card, an existing one counting as made. The lab guide lists it for a
learner as the CLI command that makes it, rendered from the script, and a test holds that command to
the real CLI. The dev seed is unchanged.

The lab comes up for a recording in about 12 seconds once `make` has built the image, where the
stack built its own image on every run. The definition's follow-up for the browser tier and
the screenshots is #1267; the epic's placeholder for simulated components with real addresses on the
lab network is #1268.
