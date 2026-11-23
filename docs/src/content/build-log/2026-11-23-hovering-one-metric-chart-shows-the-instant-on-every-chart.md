---
title: "Hovering one metric chart shows the same instant on every chart (#2424)"
date: 2026-11-23
refs: [2424, 1826, 1831, 2304, 2425]
---

A component's view and a system's view now chart every effective metric in one grid over the page's
time range, a card each (name, unit, latest value, chart), and every chart in the grid shares one
cursor: hovering one draws the crosshair and each chart's own reading at the same instant on all of
them, the way a Grafana or CloudWatch dashboard does. The keyboard drives it too (Tab to a chart,
arrows step its buckets). A metric with no sample in the range keeps its card, marked.

The cursor is a primitive (`web/src/lib/cursor.tsx`: an instant or none, shared by a provider), and
the lookup it needs is pure (`nearestByTime`, `timeAtX`, `xAtTime` in `lib/chart.ts`), so the
overlay timeline (#2425) builds on the same instant. The grid takes `{ kind, id, range }` and holds no
page logic; on the system view it replaces the Data table and its expand-a-row chart, on the
component view it sits below Vitals. Charts stay hand-drawn SVG; a `fluid` chart lays out at its
measured width so its axis text keeps reading size in a drawer-width card.

The range comes from #2304's `useTimeRange`. The series routes count `hours` back from now, so an
absolute range reads back to its start (at most 720 hours) and the chart keeps only its window.
