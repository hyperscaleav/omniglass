---
title: "A severity level declares its health effect (#2558)"
date: 2026-11-24
refs: [2558, 917]
---

`severity_level` gains `health_effect` (`none`, `degraded`, `outage`), a forward-only migration
that backfills existing rows to today's behavior and a boot seed that declares the official five
(`info` none). `health.ComponentValue` takes each open alarm's declared effect and `OutageRank` is
gone. The seed recomputes the components behind a level whose effect it changed. `/severity-levels`
returns and accepts `health_effect`; `outage` stays as a deprecated alias. The severity catalog page
shows each level's health effect and the form picks one (preselecting what the rank would derive);
an alarm whose level declares none says on the component that it does not affect health.
