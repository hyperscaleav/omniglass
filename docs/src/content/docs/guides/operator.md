---
title: Operator guide
description: "Operating an Omniglass estate day to day: the web console and the CLI, and the scope that decides what you see."
---

This is the how-to for **operating the estate**: reading and running the inventory of locations,
systems, and components you are responsible for, from either front door. It is a different job from
**administering the platform** (managing who can sign in and what they can do), which is the
[admin guide](/guides/admin/), and from **standing the platform up**, which is
[deployment](/guides/install/).

There are two ways to operate, and they are the same API with the same checks behind them:

- **[The console](/guides/console/)** is the web operator surface, served by the binary at `/web`:
  sign in, browse the inventory as a tree or a list, filter with chips, open an entity's blade, and
  edit what your grants allow. It is also a learning surface: each page teaches the concept it
  operates on.
- **[The CLI](/guides/cli/)** is the `omniglass` binary as a client of a running server, generated
  from the API so it never drifts from it. It is the path for scripting, automation, and working
  from a terminal.

## What you see is your scope

Whichever front door you use, the data is filtered to **your scope** on the server: a campus-scoped
operator sees only that campus's subtree, everywhere, automatically. You do not configure this; it
follows your [grants](/guides/admin/identity/). The console hides a surface you cannot read and the
server refuses it regardless, so what you can see is exactly what you are allowed to act on. The
model behind that is [identity and access](/architecture/identity-access/).
