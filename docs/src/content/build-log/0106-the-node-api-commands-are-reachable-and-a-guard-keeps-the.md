---
title: "The node API commands are reachable, and a guard keeps the CLI namespace honest"
date: 2026-07-22
refs: []
form: bullet
---

- **The node API commands are reachable, and a guard keeps the CLI namespace honest**
  ([ADR-0058](/architecture/decisions/#adr-0058-a-run-mode-is-a-verb-under-its-noun-and-no-command-may-be-shadowed)):
  the hand-written edge run mode and the generated `node` group both registered as `node`, so cobra
  resolved `omniglass node list` to the daemon and it failed asking for `--token`. Every generated node
  command was unreachable while the CLI guide documented them as working. The run mode is now
  `omniglass node run`. The durable half is `TestNoCommandNameCollisions`, which walks the assembled
  tree and fails on any duplicate name: written first, it found the known `node` and `type list` cases
  plus **two nobody had reported**, `grant create` and `grant delete`, where the principal-group
  variants shadow the principal ones and granting a role to a principal has no CLI path at all
  ([#357](https://github.com/hyperscaleav/omniglass/issues/357)). Those four are carried as an explicit
  list that may only shrink, since the guard also fails on an entry that stops colliding.
