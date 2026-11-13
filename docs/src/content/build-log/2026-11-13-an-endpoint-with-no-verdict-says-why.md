---
title: "An endpoint with no verdict says why (#981)"
date: 2026-11-13
refs: [981, 1187]
---

On an empty install `omniglass component reachability list` showed `verdict: null` with nothing to
say what was missing, and the path to a first verdict (create and enroll a node, run it, assign the
endpoint to it) was written only for the console. The reachability row now carries `pending`
(`no-node` or `awaiting-probe`), set only while the verdict is null and derived in the read from
whether the endpoint has a node, never stored. The Endpoints panel shows the reason beneath the row
with a link to the Nodes page or the assigned node's blade, and the operator guide gains the
command-line path. The CLI has no table renderer, so it prints the field in the read's JSON.
