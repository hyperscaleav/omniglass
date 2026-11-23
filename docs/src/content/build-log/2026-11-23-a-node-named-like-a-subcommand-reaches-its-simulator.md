---
title: "A path argument shadowed by a subcommand word is documented (#1667)"
date: 2026-11-23
refs: [1667, 1639]
---

A generated command that takes a path argument and also has children shares one position
between the argument and the children's words, and cobra matches a subcommand name first. So
`outpost simulation observation` ran the `observation` group instead of reading an outpost
named `observation`. The argument is reachable with cobra's `--` separator
(`outpost simulation -- observation`); `cmd/cligen` now says so in the command's long help, and
so does the generated CLI reference. Test: `TestArgCommandWithChildNamesTheDoubleDashEscape`.
