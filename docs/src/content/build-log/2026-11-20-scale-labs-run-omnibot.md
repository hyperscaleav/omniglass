---
title: "scale labs run omnibot"
date: 2026-11-20
refs: [2298]
---

`omniglass lab run --scenario corporate --systems N --omnibot-key-env VAR` now runs omnibot on a live
model, the same way a seeded lab does. The scale lab's server gets the key as the env-backed platform
secret `lab-omnibot-key` and the assistant's provider and key settings from the environment, so a demo
or a capture can show omnibot triaging an alarm in a fleet of hundreds or thousands of systems. Both
refusals (the plan's and the CLI's) are gone. A scale lab still seeds no omnibot recording, so without
the flag its omnibot answers only on a model the operator configures.
