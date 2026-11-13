---
title: "A bare dialed endpoint must name its target (#1187)"
date: 2026-11-13
refs: [1187, 980]
---

`POST /endpoints` on a bare dialed transport (every one but udp and the push webhook) now refuses
params that carry no `target`, or that carry `host` or `port` keys, naming `target` as `host[:port]`.
Before, `{"host":"127.0.0.1","port":8080}` returned 201 and the node then skipped the task forever
(`empty tcp target`), leaving the reachability verdict null. The check runs on create (422) and on an
edit (400, with the other bare-params checks), and the `params` field doc, and so the CLI help and
the OpenAPI, now name the shape: `target`, `count`, `timeout`, `tls-mode`. The node's skip log stays as
defence in depth. An endpoint created with no `params` at all is not yet held to the check.
