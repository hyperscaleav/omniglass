---
title: "A node runs its own simulator in process (#1198)"
date: 2026-10-27
refs: [1198, 1193]
---

`omniglass node run --simulate` runs the node's loop and the simulator side by side in one process,
under one context (`simnode.RunWithNode`). Both halves claim with the node's own name and enrollment
token, so there is no second enrollment and no second grant, and the simulated components sit on
the loopback the node collects from by construction: no namespace to share between two processes.
The two live and die together, and the first real failure is the one reported. `--impairments`,
`--port-offset` and `--poll` move under `--simulate` and are refused without it, as is `--once`.
`omniglass sim run` stays for a simulator as a process of its own. The
[simulation](/architecture/simulation/#in-the-nodes-own-process) and
[nodes](/architecture/nodes/#serving-its-own-simulated-components) pages and the install guide's
[run a simulator](/guides/install/#run-a-simulator) teach it.
