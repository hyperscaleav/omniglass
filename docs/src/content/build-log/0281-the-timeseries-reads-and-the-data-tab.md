---
title: "The timeseries reads and the Data tab"
date: 2026-08-26
refs: []
form: bullet
---

- **The timeseries reads and the Data tab**
  ([#794](https://github.com/hyperscaleav/omniglass/issues/794)). The platform's first
  series-history reads: `GET /{components,systems}/{name}/metrics/{metric}/samples` and
  the property mirror at `.../properties/{property}/samples`, raw rows newest first,
  windowed (hours) and capped (limit, newest kept), extending the effective reads' route
  grammar; downsampling waits for a measured need. Over them, the `TimeseriesChart`
  primitive (a pure `chartLayout` core, one series, area fill, recessive grid, the newest
  point emphasized) and the workspace's Data tab, one picker chip per declared metric;
  the dev seed grows a day of temperature and occupancy on the huddle so the chart is
  judgeable. The chart primitive is dashboards' seed (primitive first).
