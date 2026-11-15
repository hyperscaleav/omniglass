---
title: "The soak verdict is the platform's own alarms and loss ratios (#1988)"
date: 2026-11-15
refs: [1988, 1623, 1633]
---

`omniglass lab soak verdict --since <ts>` judges a week-long soak from what the platform says about
itself, read through its API alone: the platform system's alarm history and every loss ratio on the
Platform health page. It passes when the soak ran 7 days, no alarm is open, none was open past a
stated tolerance (30 minutes), and every loss ratio reads 0; a reading that cannot answer (no build,
no loss ratio, a token that sees part of the page, an alarm history cut at the API's cap) fails
rather than passes. The judge is a pure function over fixed alarm histories, with the API read behind
a getter, and an import-ban test keeps the database and the harness out of it. The command writes the
release's record, one file per build, and the release process now holds that no passing record for the
build means no release. Running the soak in CI and scheduling the week stay manual.
