---
title: "A heartbeat cut short by shutdown is not audited (#2274)"
date: 2026-11-22
refs: [2274]
---

`TestRunAuditsAChangeOfOutcomeNotEveryBeat` flaked: a beat in flight when the run's context was
canceled failed with `context canceled`, which counted as a change of outcome and wrote a third
audit row. A send that fails because the run's own context ended is not an outcome of the receiver,
so `Beat` now returns the context error without auditing it. A regression test pins it.
