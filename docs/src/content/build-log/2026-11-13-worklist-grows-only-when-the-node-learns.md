---
title: "A node's worklist grows only when the node learns (#1872)"
date: 2026-11-13
refs: [1872, 1583, 1623]
---

On the 3,000-system corporate soak, the headquarters node's compressed worklist went from 938 KB
to 1,171 KB with no operator change. The whole growth was the https key pins the node trusted on
first use: 440 endpoints in the first minute after boot. Each pin rides every task of its endpoint
(about 11), and the 4,831 https tasks gained 71 bytes each, exactly the 343,001 raw bytes the reply
gained. The server's log shows the climb: the first reply at 938,301 bytes with no pin, the first
refusal 11 seconds later at 1,105,311 bytes with 3,289 tasks already pinned, three more steps, then
1,170,922 bytes on every pull for the rest of the soak. So the worklist does not drift with time;
once past the 1 MB limit it is refused on every pull, so the node runs its last worklist. The nodes
page now names the two learned fields, the key pin and the last rejection. A storage test holds two
builds of an unchanged worklist to the same bytes, before and after a pin is learned and a
rejection is recorded. The fix for the repeated pin is the endpoint-grouped wire, #1583.
