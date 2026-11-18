---
title: "A component nothing has observed reads unknown, not healthy"
date: 2026-11-14
status: "Accepted"
summary: "The component's own verdict (the alarms read, the fleet view's dots, a system's members) says unknown until an alarm was raised on it or an endpoint delivered. The rollup's vocabulary is unchanged"
refs: [1454, 933, 626]
---

- **Date:** 2026-11-14 | **Status:** Accepted | **Pages:** [health](/architecture/health/#a-component-nothing-has-observed)
- **Decision:** (#1454.) The component verdict reads (`GET /components/{name}/alarms`, the fleet view's dots, a system's members read) answer `unknown` for a component with no recorded verdict, no alarm ever raised on it (cleared ones count), and no endpoint that has delivered anything (`endpoint_live`). One SQL expression (`componentObservedSQL`) decides it, so the three reads agree. The page of a device nothing has collected from and the Explore outline now read the same word.
- **Consequences:** The alarms read's `verdict` gains a fourth value, `unknown`, so a client switching on it must handle one more. The console already words anything outside the four it knows as its grey `unknown` chip (`verdictOf`), so no UI vocabulary changed. The rollup is untouched: system and location verdicts come from the recorded rows and never see `unknown`, so an unobserved member neither raises nor masks a system's verdict. A component in a system that had read `healthy` with no data now reads `unknown` until its first delivery or alarm.
- **Context:** The page badge read `healthy` for a component nothing had observed because the alarms read took "no active alarms" to mean healthy, while the Explore drawer, fed by a view with no dot for a loose component, read `unknown`. The issue offered two fixes: make the server verdict distinguish never-observed, or have the page read the outline's source. The first was chosen because the verdict is server-sent (`web/src/lib/health.ts`) and the page's poll should carry it.
- **Not chosen:**
  - **The page reads the fleet view's dot instead of the alarms read.** It would drop the live poll the page relies on (#934), and the dot read healthy for a system member with no data, so the two surfaces would still disagree there.
  - **Adding `unknown` to the rollup's `health.Verdict`.** How `unknown` composes upward is still an open design question on the health page; this slice names only the never-observed reading and leaves the composition to that decision.
