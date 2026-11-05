---
title: "The lab node starts under rootless Docker (#1506)"
date: 2026-11-05
refs: [1506]
---

The lab's node container asked for `net.ipv4.ping_group_range="0 2147483647"`, every group there is.
Under rootless Docker the container's user namespace maps only 65537 groups (the user's own plus the
65536 of `/etc/subgid`), and the kernel refuses a range naming an unmapped group, so runc failed with
`invalid argument` and the node never started. The range is now `0 65535`: it admits root, which the
node runs as, and distroless nonroot (65532), and fits any standard mapping. The same value goes to
`make test-netadmin` and the e2e probe container. The CI runner's host-wide grant is untouched: it is
set as root on the host's own namespace, where the full range is valid.
