---
title: "omnibot runs on a live model"
date: 2026-11-18
refs: [2109]
---

omnibot answers on a real model. An OpenAI-compatible adapter (chat completions with tool calls)
sits behind the loop's provider seam, GitHub Models by default, and the `assistant` settings name it:
`provider`, `model`, `base_url`, and `key_secret`, a `$platform` api-key secret by name. The server
picks the model at the start of each run (the configured provider, else the recording, else a 503),
so a settings change is the next run's with no restart; it unseals the key in process and sends it
only as the model call's bearer, and a test holds every event, read and log line of a run, a refused
key that the provider echoes included, free of it. A provider failure ends the run on an error that
names it. `omniglass omnibot` now runs its conversation on the server and renders the stream on the
terminal, confirming an `ask` inline and answering it with a resume run, so the CLI never needs the
key either; `--replay` keeps the local loop for tests and tapes. `omniglass lab run
--omnibot-key-env GITHUB_TOKEN` runs a lab live with one flag.
