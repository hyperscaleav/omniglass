---
title: "An agent triages the fleet's alarms over MCP (#2025)"
date: 2026-11-16
refs: [2025, 1162]
---

The MCP catalog grows from 66 tools to 98, enough for an agent to triage: find what is alarming, acknowledge or snooze it, read the events and rules behind it, and fix the component, its tags, its place or its system. Finding what is red needed a route that did not exist, so `GET /alarms` (`list-alarms`) now lists the active alarms across every component the caller may read, worst severity first, with the Storage Gateway injecting the caller's component scope. The catalog is also credential-blind now. Fourteen operations that take or return a credential in plaintext carry an `x-omniglass-plaintext` stamp. The generator refuses a tool on any of them, or on any operation gated on a reveal, and a spec test fails when a credential-shaped operation goes unstamped. `create-secret` leaves the catalog as a result. Running an endpoint's poll now masks the endpoint's own secrets wherever the device echoes them in the answer: a sample's text, labels and identity, the faults, and a refusal's text.
