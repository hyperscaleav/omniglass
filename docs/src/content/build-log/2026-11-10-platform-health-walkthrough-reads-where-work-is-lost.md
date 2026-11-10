---
title: "The Platform health walkthrough reads where work is lost and the simulator's gaps (#1730)"
date: 2026-11-10
refs: [1730, 1646, 1647]
---

The [Platform health](/guides/operator/platform-health/#walkthroughs) walkthrough is re-recorded against
the page as it is on main. After the header it waits for both hops under where work is lost to read 0%,
captioned as the share each hop took in and did not hand on, then opens the node hop to read the keys it
is read from and each node's own share. Past the node table it stops on the simulator's gap table, each
gap a loss ratio where 0 is healthy. The script test pins both stops, so a later edit that drops the hop
or the simulator section, or their loss-ratio captions, fails `make test-web`.
