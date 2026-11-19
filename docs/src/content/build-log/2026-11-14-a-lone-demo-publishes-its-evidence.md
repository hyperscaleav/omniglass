---
title: "A run:demo of one demo publishes its evidence again (#1885)"
date: 2026-11-14
refs: [1885, 1617]
---

Since `actions/download-artifact` moved to v8 (#1617), a `run:demo` that selected exactly one demo recorded it, then called it never run and failed at publish: v8 extracts a pattern's lone match into the download path itself, with no directory named for the artifact, so the combine job found no `demo-leg-<id>/` to read. Each record job now uploads its leg already wrapped in that directory and combine merges the legs into one path, so every leg lands at the same place whether one demo is selected or many. A run where no demo recorded now commits an empty evidence tree and posts its comment, where it used to fail on the missing tree. `web/src/demo-workflow.test.ts` plays v8's extract rule against the workflow's own steps for one leg and for several.
