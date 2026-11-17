---
title: Visualization survey
description: Every chart, sparkline, metric card, status pill, health indicator, inline-viz table and log or event list in the console today, where each gets its data, how live it is, and the dashboard widget it maps to.
---

This is the survey behind the dashboard canvas ([epic #1826](https://github.com/hyperscaleav/omniglass/issues/1826),
issue #1827): what the console draws today, so each surface can be rebuilt as a widget on the canvas
and nothing is left behind. It is a **point-in-time, hand-written survey** of `web/src/` at commit
`e953d9bf` (2026-11-15). It restates facts the code knows, so it goes stale as the console changes;
the widget library that epic #1826 builds replaces it, and until then a surface added after that
commit is not in it. Paths are relative to `web/src/`.

## How to read it

An **entry** is one place the console draws a visualization: one component mounted by one surface
(a page, or a blade that several pages host). Each carries six facts:

| Field | Meaning |
|---|---|
| Component | The component or function that draws it, with its file. |
| Pages | Every console route that shows it (SPA routes, not API routes, so a path parameter is written `<id>`). Blades (the stacked detail panels) are hosted by `Explore`, `LocationZoom`, `SystemZoom` and `ComponentLeaf` through `fleetRegistry`, so a blade entry lists those four routes. |
| Primitive | What it shows: `metric`, `property`, `event`, `log`, or `derived` (a count or verdict the server computed from those: the fleet view, the health read, the alarm read). |
| Source | The API read or helper that feeds it. |
| Live behavior | `poll N s` is a `refetchInterval`; `mount+focus` is TanStack's default (fetch on mount and again when the window regains focus, no interval); `static` is read once with focus refetch off; `follows parent` means it draws what its host already fetched. |
| Widget | The widget in epic #1826's vocabulary (metric chart, sparkline row, metric card, property distribution, property timeline, event list, log list, timeline), or `new: <name>` where the vocabulary has nothing that fits. |

The poll constants, for reference: `LEAF_POLL_MS` 5 s and `LEAF_OUTPOST_POLL_MS` 10 s
(`lib/component_leaf.ts`), `SERIES_POLL_MS` 30 s (`lib/series.ts`), `ALARM_TIMELINE_POLL_MS` 5 s
(`lib/alarms.ts`), `INBOX_POLL_MS` 30 s (`lib/inbox.ts`), `SIDEBAR_POLL_MS` 10 s and
`PAGE_POLL_MS` 1 s (`lib/simulation.ts`), and 10 s in `RecentNotifications` and the three rule lists.

### What counts

Included: anything that shows a **state or a measurement** of a monitored thing, or a list of what
happened to it: charts, sparklines, figures and counts, verdict, severity, liveness and simulation
pills, state-over-time strips, tables whose cells carry those, and event, alarm, notification, audit
and log lists.

Excluded, because they state a fact about configuration, not about the fleet: origin chips
(`official` / `custom` / `overridden`), type and kind chips, tag and role chips, `enabled` /
`disabled` and `active` / `paused` flags on rules, targets and subscriptions, tier and provenance
badges (`ProvenanceDot`, the Properties panel), the lab banner, and credential lists
(`SessionsList`). These recur on most admin pages; they are checked in the [page list](#every-page)
and not given entries.

## Charts and sparklines

`TimeseriesChart` (`components/TimeseriesChart.tsx`) is the only chart in the console, drawn from the
pure geometry in `lib/chart.ts`. Full mode is 640 by 170 with axes, crosshair and a min/max band over
the average line; spark mode is 160 by 36 with no chrome. It is imported by exactly two pages.

| ID | Component | Pages | Primitive | Source | Live behavior | Widget |
|---|---|---|---|---|---|---|
| V01 | `TimeseriesChart spark` in the server panel `Row` (`pages/PlatformHealth.tsx`) | `/platform` | metric | `metricBuckets("components", server, key, 1 h, 60 s)` via `panelSeriesRequest` (`lib/platformhealth.ts`, `lib/series.ts`) | poll 30 s, only while the server's disclosure is open and the key is reported | sparkline row |
| V02 | `TimeseriesChart` full, the expanded panel row (`pages/PlatformHealth.tsx`) | `/platform` | metric | the same series query as V01 | poll 30 s, shares V01's query | metric chart |
| V03 | `TimeseriesChart spark` in the data table (`pages/SystemZoom.tsx`, `data-testid="data-tab"`) | `/systems/<id>` | metric | `metricBuckets("systems", id, metric, 24 h, 300 s, instance)` per KPI metric, over `systemMetrics` (`lib/series.ts`) | poll 30 s; the metric list polls 5 s | sparkline row |
| V04 | `TimeseriesChart` full, the expanded data row (`pages/SystemZoom.tsx`) | `/systems/<id>` | metric | the same series query as V03 | poll 30 s, shares V03's query | metric chart |

## Metric cards and figures

| ID | Component | Pages | Primitive | Source | Live behavior | Widget |
|---|---|---|---|---|---|---|
| V05 | `Header` `Fact` cards: platform verdict, version, up for (`pages/PlatformHealth.tsx`) | `/platform` | derived | `platformView` (`lib/platformhealth.ts`, `GET /views/platform`) | poll 5 s | metric card |
| V06 | `Stat` tiles: locations, open alarms, systems, collectors, platform verdict (`pages/Home.tsx`) | `/` | derived | `fleetView`, `listLocations`, `listOutposts`, `platformView` | mount+focus | metric card |
| V07 | `uptime-kpi` card, uptime over 30 days (`pages/SystemZoom.tsx`) | `/systems/<id>` | derived | `systemHealth` transitions (`lib/health.ts`) | poll 5 s | metric card |
| V08 | `leaf-vitals` list, the latest value per effective metric with a live dot and per-endpoint spread (`pages/ComponentLeaf.tsx`) | `/components/<id>` | metric | `effectiveMetrics` (`lib/component_leaf.ts`) | poll 5 s | metric card (no series; the only metric surface with no chart or sparkline) |
| V09 | `Count` per catalog row (`pages/CatalogOverview.tsx`) | `/catalog` | derived | each catalog's own list read, on the list page's query key | mount+focus | metric card |
| V10 | `explore-counts` line and the "need attention" count (`components/OutlineWorkspace.tsx`) | `/explore`, `/locations/<id>`, `/systems/<id>` | derived | `fleetView`, plus `systemHealth` per troubled system (stale 30 s) | mount+focus | metric card |
| V11 | `InboxIndicator` unread count on the top bar (`components/InboxIndicator.tsx`) | every page in the shell | event | `useUnreadCount` (`lib/inbox.ts`) | poll 30 s, and at once when an entry is marked read | metric card |
| V12 | `RuleLive` tallies: in force, open, pending (`pages/Rules.tsx`) | `/event-rules` | derived | `listEventRules` | poll 10 s | metric card |
| V13 | `CalcLive` tallies: computed, pending, without inputs, failing (`pages/CalcRules.tsx`) | `/calc-rules` | derived | `listCalcRules` | poll 10 s | metric card |

## Status pills and health indicators

Every verdict surface wears a distinct hue and a glyph, so a state never rides on color alone.
`HealthBadge`, `SeverityBadge`, `SimStatusBadge`, `SessionPill` and `NotificationStatusPill` are the
component set; the surfaces below are where they are mounted.

| ID | Component | Pages | Primitive | Source | Live behavior | Widget |
|---|---|---|---|---|---|---|
| V14 | `HealthBadge` sm, header (`components/HealthBadge.tsx`, in `pages/SystemZoom.tsx`) | `/systems/<id>` | derived | `systemHealth` | poll 5 s | new: verdict badge |
| V15 | `HealthBadge` sm, header (`pages/ComponentLeaf.tsx`) | `/components/<id>` | derived | the fleet view's recorded verdict | mount+focus | new: verdict badge |
| V16 | `HealthBadge` md, outpost rows xs, per-server disclosure with an "N open" badge (`pages/PlatformHealth.tsx`) | `/platform` | derived | `platformView` | poll 5 s | new: verdict badge |
| V17 | `HealthBadge` in the system, component and location blades (`components/EntityBlade.tsx`) | `/explore`, `/locations/<id>`, `/systems/<id>`, `/components/<id>` | derived | `fleetView`, `systemHealth`, `locationHealth` | mount+focus (health reads refetch off on focus when standalone) | new: verdict badge |
| V18 | `SystemSummary` card: verdict, since-line, why rows, slot text (`components/SystemSummary.tsx`) | `/locations/<id>` | derived | `fleetView`, `systemHealth` (stale 30 s) | mount+focus | new: system summary card |
| V19 | `HealthGlyph` and `HealthSlots`, the four-slot verdict counts on each outline row (`components/Outline.tsx`, `components/OutlineWorkspace.tsx`) | `/explore`, `/locations/<id>`, `/systems/<id>` | derived | `fleetView`, the verdicts the server recorded | mount+focus | new: verdict slots |
| V20 | `MemberRows` glyph and alarm text per member (`components/MemberRows.tsx`) | `/systems/<id>` | derived | `systemMembers` | poll 5 s | new: verdict badge |
| V21 | `OutpostTable`: liveness pill, `HealthBadge` xs, tinted figure cells, row tone (`pages/PlatformHealth.tsx`) | `/platform` | derived | `platformView` | poll 5 s | new: status table |
| V22 | `StatusPill` up / down / never, in the list and the outpost blade (`pages/Outposts.tsx`) | `/outposts` | derived | `listOutposts`, `nodeStatus` from `last_heartbeat_at` against the down window | mount+focus (the age is computed client-side) | new: status pill |
| V23 | `SimStatusBadge` and hit counters per row, plus `OutpostCard` connected and impairment pills (`pages/Simulation.tsx`, `components/SimStatusBadge.tsx`) | `/simulation` | derived | `getSimulation` (`lib/simulation.ts`) | poll 1 s | new: simulator status table |
| V24 | `SimulationSection` and `SimComponentDrawer` with `SimStatusBadge` (`components/SimulationSection.tsx`, `components/SimComponentDrawer.tsx`) | `/components/<id>`; `/simulation` (drawer) | derived | `getComponentSim` | poll 5 s (section), poll 2 s (drawer) | new: simulator status table |
| V25 | `PlatformSimulation` gap, protocol and device tables with reading and state badges (`pages/PlatformSimulation.tsx`) | `/platform` | derived | `platformSimulation`, `getSimulation` | poll 5 s, and the sidebar cadence for the fleet read | new: simulator status table |
| V26 | `ReachabilityPanel` row: `ReachLadder`, `SessionPill` and the responding / down / stale / unknown pill (`components/ReachabilityPanel.tsx`) | `/components/<id>` (Configure tab), blades on the four fleet routes | derived | `getReachability` (`lib/reachability.ts`) | mount+focus, refreshed by the host's poll of the same key | new: reachability ladder |
| V27 | `ReachLadder` and `SessionPill` in the Collection section (`pages/ComponentLeaf.tsx`) | `/components/<id>` | derived | `getReachability`; `getOutpost` per outpost | poll 5 s (reach), poll 10 s (outposts) | new: reachability ladder |
| V28 | `ReachLadder` and `SessionPill` in the endpoint blade (`components/endpointBlades.tsx`) | `/explore`, `/locations/<id>`, `/systems/<id>`, `/components/<id>` | derived | `getReachability` | poll 5 s | new: reachability ladder |
| V29 | `SlotsPanel` staffing badges: understaffed, short, spare, impact, down chips (`components/SlotsPanel.tsx`) | `/systems/<id>` (Configure tab), system blades | derived | `systemSlots`, `systemHealth` | mount+focus (focus refetch off) | new: slot staffing badges |
| V30 | `ReconciliationPanel` drift badge (`components/ReconciliationPanel.tsx`) | `/systems/<id>` and `/components/<id>` (Configure tab), blades | property | `ownerProperties` (`lib/properties.ts`) | mount+focus (focus refetch off) | new: drift badge |

## Timelines and state-over-time strips

`StateStrip` (`components/StateStrip.tsx`) over `lib/timeline` spans is the one state-over-time idiom.

| ID | Component | Pages | Primitive | Source | Live behavior | Widget |
|---|---|---|---|---|---|---|
| V31 | `HealthHistory compact` with `incident-marker-*` triangles and the expandable `incident-list` (`components/HealthHistory.tsx`, in `pages/SystemZoom.tsx`) | `/systems/<id>` (Activity tab) | event (recorded verdict edges) | `systemHealth` transitions | poll 5 s; the clock ticks every 60 s | timeline |
| V32 | `AvailabilityStrip`, up / down segments with an uptime figure (`components/ReachabilityPanel.tsx`) | `/components/<id>` (Configure tab), blades on the four fleet routes | event (reachability edges) | `getReachability` history | mount+focus | timeline |
| V33 | `AlarmTimeline`, one alarm's lifecycle with kind chips and `NotificationStatusPill` (`components/AlarmTimeline.tsx`) | `/systems/<id>`, `/components/<id>`, `/event-rules`, blades and the Alarms panel on the four fleet routes | event | `alarmTimeline` (`lib/alarms.ts`) | poll 5 s, focus refetch off, only while expanded | timeline |

## Alarm, event and log lists

| ID | Component | Pages | Primitive | Source | Live behavior | Widget |
|---|---|---|---|---|---|---|
| V34 | `alarm-strip` "Why": `SeverityBadge` and `AlarmTimeline` (`pages/SystemZoom.tsx`) | `/systems/<id>` | event | `systemAlarms` | poll 5 s | new: alarm list |
| V35 | `incident-list` and "Other alarms" (`pages/SystemZoom.tsx`) | `/systems/<id>` (Activity tab) | event | `systemAlarms`, `systemHealth` | poll 5 s | new: alarm list |
| V36 | `leaf-alarms` "Why": `SeverityBadge`, `SnoozedBadge`, `AlarmTimeline` (`pages/ComponentLeaf.tsx`) | `/components/<id>` | event | `componentAlarmsRead` (`lib/alarms.ts`) | poll 5 s | new: alarm list |
| V37 | `AlarmsPanel`: grouped by severity, unacknowledged badge, `AlarmTimeline` (`components/AlarmsPanel.tsx`) | `/components/<id>` (Configure tab), component blades | event | `componentAlarms` | mount+focus (focus refetch off) | new: alarm list |
| V38 | Rule detail "Open alarms" and "conditions in force" lists with `SeverityBadge` and `AlarmTimeline` (`pages/Rules.tsx`) | `/event-rules` (blade) | event | `getRuleState` | poll 5 s | new: alarm list |
| V39 | `events-tab` with `RuleLink` and alarm timeline toggles (`pages/SystemZoom.tsx`) | `/systems/<id>` (Activity tab) | event | `systemEvents` | poll 5 s, only while the tab is open | event list |
| V40 | `EventsPanel`: key badge, origin badge, time (`components/EventsPanel.tsx`) | `/components/<id>` (Activity tab) | event | `getEvents` | mount+focus | event list |
| V41 | `logs-tab` mono block with `LogFilters`, severity-coloured lines (`pages/SystemZoom.tsx`) | `/systems/<id>` (Activity tab) | log | `systemLogs` | poll 5 s, only while the tab is open | log list |
| V42 | `LogsPanel` (`components/LogsPanel.tsx`) | `/components/<id>` (Activity tab) | log | `getLogs` | mount+focus | log list |
| V43 | `LogsPanel` "Self-logs" with a query override (`pages/Outposts.tsx`) | `/outposts` (outpost blade) | log | `getOutpostLogs` | mount+focus | log list |
| V44 | Inbox list with the raised / cleared transition pill (`pages/Inbox.tsx`) | `/inbox` | event | `listInbox` (`lib/inbox.ts`) | mount+focus; the top-bar count (V11) polls 30 s | event list |
| V45 | `RecentNotifications` with `NotificationStatusPill` (`components/RecentNotifications.tsx`) | `/action-rules`, `/notification-targets` (blades) | event | `listRecentNotifications` | poll 10 s | event list |
| V46 | Audit log table with the verb badge and a before / after diff drawer (`pages/Audit.tsx`) | `/audit` | event | `listAuditLog` (`lib/audit.ts`), paged by cursor | mount+focus; loads older pages on demand | new: audit list |
| V47 | `CommandsPanel`: phase badge per command, detail, in-flight count (`components/CommandsPanel.tsx`) | `/components/<id>` (Activity tab) | event | `componentCommands` (`lib/commands.ts`) | poll 1 s while a command is in flight, else 5 s | new: command list |

## Other

| ID | Component | Pages | Primitive | Source | Live behavior | Widget |
|---|---|---|---|---|---|---|
| V48 | `PipelineFlow`, the "where work is lost" hop flow with a lane tab group and a per-hop loss figure (`components/PipelineFlow.tsx`) | `/platform` | derived (counts of metric, property, event and log samples per hop) | `view.pipeline` from `platformView` | follows parent (poll 5 s) | new: pipeline flow |
| V49 | `SystemMap`, the top-down room map with verdict-coloured markers (`components/SystemMap.tsx`, `lib/system_map.ts`) | `/systems/<id>` | derived | `systemMembers`, `layoutMarkers` | poll 5 s | new: floor map |

## Every page

Each of the 51 files under `pages/` is either a surface with entries or checked and without one.
The four fleet routes (`/explore`, `/locations/<id>`, `/systems/<id>`, `/components/<id>`) also host the
blade entries V17, V26, V28, V29, V30, V32, V33 and V37 through `fleetRegistry`; the rows below list
what each page draws itself.
"Config chips only" means the page shows only the excluded attribute chips described above.

| Page | Route | Result |
|---|---|---|
| `ActionRules.tsx` | `/action-rules` | V45 (the rule list's enabled flag is a config chip) |
| `Audit.tsx` | `/audit` | V46 |
| `CalcRules.tsx` | `/calc-rules` | V13 |
| `CatalogOverview.tsx` | `/catalog` | V09 |
| `CommandTypes.tsx` | `/command-types` | none checked: config chips only |
| `ComponentLeaf.tsx` | `/components/<id>` (via `Components.tsx`) | V08, V15, V24, V26, V27, V32, V33, V36, V37, V40, V42, V47 |
| `ComponentTypes.tsx` | `/component-types` | none checked: config chips only |
| `Components.tsx` | `/components/<id>` | wrapper: mounts `ComponentLeaf`, or the create form for the create route (`components/create`, no leading slash) |
| `Drivers.tsx` | `/drivers` | none checked: config chips only |
| `EventTypes.tsx` | `/event-types` | none checked: config chips only |
| `Explore.tsx` | `/explore` | V10, V19 (blades only otherwise) |
| `Files.tsx` | `/files`, `/files/<id>` | none checked: config chips only (`Sensitive`, content type) |
| `FleetRedirect.tsx` | `/fleet`, `/locations`, `/systems`, `/components` | none checked: redirects to `/explore` |
| `GroupDetail.tsx` | `/user-groups` (blade) | none checked: member list with kind chips |
| `Groups.tsx` | `/user-groups` | none checked |
| `Home.tsx` | `/` | V06 |
| `Inbox.tsx` | `/inbox` | V44 |
| `LocationTypes.tsx` | `/location-types` | none checked: config chips only |
| `LocationZoom.tsx` | `/locations/<id>` (via `Locations.tsx`) | V10, V18, V19 |
| `Locations.tsx` | `/locations/<id>` | wrapper: mounts `LocationZoom` |
| `Login.tsx` | `/login` | none checked: a form |
| `Metrics.tsx` | `/metrics` | none checked: config chips only (data type, origin) |
| `NotFound.tsx` | `*` | none checked: static page |
| `NotificationTargets.tsx` | `/notification-targets` | V45 (enabled flag and channels are config chips) |
| `Notifications.tsx` | `/notifications` | none checked: config chips only |
| `Outposts.tsx` | `/outposts` | V22, V43 |
| `PlatformHealth.tsx` | `/platform` | V01, V02, V05, V16, V21, V25, V48 |
| `PlatformSimulation.tsx` | embedded in `/platform` | V25 |
| `Products.tsx` | `/products` | none checked: config chips only |
| `Profile.tsx` | `/profile` | none checked: credential lists and permission chips |
| `Properties.tsx` | `/properties` | none checked: config chips only (data type, origin) |
| `RoleDetail.tsx` | `/roles` (blade) | none checked: permission list |
| `Roles.tsx` | `/roles` | none checked |
| `Rules.tsx` | `/event-rules` | V12, V33, V38 |
| `SecretTypes.tsx` | `/secret-types` | none checked: config chips only |
| `Secrets.tsx` | `/secrets` | none checked: config chips only |
| `SectionStub.tsx` | `/alarms`, `/config`, `/dashboards`, `/learn`, `/log-types` | none checked: placeholder page (`/dashboards` is where the canvas lands) |
| `Settings.tsx` | `/settings` | none checked: effective and lock chips are config |
| `SeverityLevels.tsx` | `/severity-levels` | none checked: `SeverityBadge` here previews a configured level's look, not an alarm's state |
| `Simulation.tsx` | `/simulation` | V23, V24 (drawer) |
| `Standards.tsx` | `/standards` | none checked: config chips only |
| `Subscriptions.tsx` | `/profile/subscriptions` | none checked: `SeverityBadge` on the preview of what a draft subscription would match, and the active / paused flag, are config |
| `SystemTypes.tsx` | `/system-types` | none checked: config chips only |
| `SystemZoom.tsx` | `/systems/<id>` (via `Systems.tsx`) | V03, V04, V07, V10, V14, V19, V20, V29, V30, V31, V33, V34, V35, V39, V41, V49 |
| `Systems.tsx` | `/systems/<id>` | wrapper: mounts `SystemZoom` |
| `Tags.tsx` | `/tags` | none checked: config chips only |
| `Templates.tsx` | `/templates` | none checked: config chips only |
| `UserDetail.tsx` | `/users` (blade) | none checked: user kind chips and grants |
| `Users.tsx` | `/users` | none checked: config chips only |
| `Variables.tsx` | `/variables` | none checked: config chips only |
| `Vendors.tsx` | `/vendors` | none checked: config chips only |

## What the survey shows

- **One chart, two pages.** `TimeseriesChart` backs every chart and sparkline, on `/platform` and
  `/systems/<id>` only, and only for `metric` buckets. `lib/series.ts` also exports `metricSeries`
  (raw samples), `propertySeries` and `propertyBuckets`, and no page calls them: the property
  distribution and property timeline widgets have **no existing call site** to migrate, and the
  `step` mode the chart supports for them is unused today.
- **A component has no history.** `/components/<id>` shows the latest value of each metric (V08) and
  never a chart or sparkline, though the series endpoints take `components` as an owner (V01 already
  charts a component's metrics on `/platform`).
- **Liveness is uneven.** Entries on `/platform` and `/systems/<id>` poll every 5 s and their series
  every 30 s. The component's events and logs (V40, V42), the Home tiles (V06), the outline counts
  (V10, V19) and the Inbox list (V44) read on mount and on focus only, so they go stale on a wall
  display. A widget library that owns its refresh cadence closes this gap.
- **Verdict surfaces are the long tail.** Fourteen entries are a verdict, severity or liveness pill in
  a different frame (V14 to V22, V26 to V30); they share `HealthBadge`, `SeverityBadge` and the
  `VERDICT_HUE` glyph vocabulary, so the new widgets they need are one family, not fourteen.

## Widget types

Deduplicated by widget, with the number of call sites (entries above; the 49 entries sum to 49). A `new:` row is a widget the
epic's vocabulary does not yet name; the count says how much of the console it would carry.

| Widget | Call sites | Entries |
|---|---|---|
| metric card | 9 | V05, V06, V07, V08, V09, V10, V11, V12, V13 |
| event list | 4 | V39, V40, V44, V45 |
| log list | 3 | V41, V42, V43 |
| timeline | 3 | V31, V32, V33 |
| sparkline row | 2 | V01, V03 |
| metric chart | 2 | V02, V04 |
| property distribution | 0 | none (`propertyBuckets` exists and is unused) |
| property timeline | 0 | none (`propertySeries` exists and is unused) |
| new: verdict badge | 5 | V14, V15, V16, V17, V20 |
| new: alarm list | 5 | V34, V35, V36, V37, V38 |
| new: reachability ladder | 3 | V26, V27, V28 |
| new: simulator status table | 3 | V23, V24, V25 |
| new: status table | 1 | V21 |
| new: status pill | 1 | V22 |
| new: system summary card | 1 | V18 |
| new: verdict slots | 1 | V19 |
| new: slot staffing badges | 1 | V29 |
| new: drift badge | 1 | V30 |
| new: audit list | 1 | V46 |
| new: command list | 1 | V47 |
| new: pipeline flow | 1 | V48 |
| new: floor map | 1 | V49 |
