---
title: "omniglass platform health reads the platform from the CLI (#2055)"
date: 2026-11-17
refs: [2055, 1623]
---

Whether Omniglass was keeping up could be read on the console's Platform health page, or measured by
`lab measure` from the lab's own host; nothing read it from a terminal pointed at a server you did
not start. `omniglass platform health` reads the platform view through the API with a token and
reduces it to one table, or `--json`: the verdicts, the open alarms, each hop's loss and the end to
end, the nodes with loss, and every failure above zero. Its exit status is the verdict, nonzero when
anything it reads is not healthy or a part is not visible to the token, which prints as not visible
rather than as zero. `--window`, `--every` with `--append`, and `--page-clients` take the lab
sampler's measurements against any server, sharing its sampling clock and its system page load,
which moved out of the lab harness to be reused. `TestPlatformHealthExitsNonzeroOnAnOpenAlarm` and
`TestPlatformHealthSaysWhatTheCallerCannotSee` run the command against a real server and Postgres.
