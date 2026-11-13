---
title: "Fire-and-forget commands refuse settle from response (#908)"
date: 2026-11-12
refs: [908]
---

A driver spec that binds a fire-and-forget command (one setting no target value, such as `reboot`) and declares `settle: { from: response }` is now refused at write with the message "command X is fire-and-forget: it settles at its issue, so it declares no settle".

Fire-and-forget commands settle when their execution report arrives or times out (at issue), not from the device's reply. A settle declaration reading the response is moot and signals confusion in the spec's authoring.

Updated the drivers admin guide settle section to explain the rule: fire-and-forget commands declare no settle, settleable commands declare how they judge the reply or read the next observation.
