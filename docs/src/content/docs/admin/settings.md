---
title: Platform settings
description: "The install-wide settings on Admin > Settings: how long each kind of data is kept, how often an endpoint reports on itself, and where the other namespaces are taught."
sidebar:
  label: Settings
  order: 7
  badge:
    text: Partial
    variant: note
---

**Admin > Settings** holds the install-wide settings, grouped in **namespaces** (`retention`,
`collection`, `notification`, `agent`, and the rest). Reading them takes `settings:read`; changing one
takes `settings:update` plus `platform:update`, since a platform setting moves every location at
once. Each value shows which level it comes from (the declared default, the server's settings file,
or an override set here), and restoring one drops the override. The model underneath, including how a setting is declared once in Go
and validated from that one definition, is [settings](/architecture/settings/).

From the CLI, `omniglass setting list` reads every namespace, and `omniglass setting update` applies a
merge patch to one, where a `null` restores a key:

```sh
omniglass setting update retention --body '{"metric_days": 30}'
omniglass setting update retention --body '{"metric_days": null}'
```

This page teaches the two namespaces that shape what your fleet keeps. The others are taught where
they act: `notification` (the mail relay and `egress_allow`) on
[notifications](/guides/admin/notifications/), and `agent` (what an AI agent may do unasked) on
[agents](/guides/agents/).

## How long data is kept

The server keeps each sample lane for a window of days and removes what is older, a little at a
time, every hour (every minute while it is catching up). The windows are the `retention` namespace,
each from 1 to 36,500 days:

- `metric_days`, 14 by default: how long a metric sample (a reading such as a round-trip time) is
  kept, at least. Samples are stored by UTC day and usually removed a whole day at a time, so a
  sample can stay up to a day past the window, until its day goes.
- `property_days`, 90 by default: how long a property transition (a change such as a power state
  going from on to off) is kept.
- `trend_days`, 730 by default: how long the hourly trends of each metric (count, minimum, maximum,
  sum, first and last value of every hour) are kept, counted from the hour, like `metric_days`. It is
  a window of its own: set it below `metric_days` and an hour's trend goes before its samples do.
- Log lines, from your components and from your outposts, by severity:
  - `log_debug_days`, 3 by default;
  - `log_info_days`, 14 by default, which also covers notice and a line with no severity;
  - `log_warning_days`, 30 by default;
  - `log_error_days`, 90 by default, which also covers critical, alert and emergency.
- Your outposts' own logs, if you want them kept for a different window than your components' lines:
  `outpost_log_debug_days`, `outpost_log_info_days`, `outpost_log_warning_days` and
  `outpost_log_error_days`. Each reads **not set** until you set it, and while it is not set an
  outpost's lines of that severity follow the shared window above; restore one to go back to it. Drop
  an outpost's debug chatter after a day while keeping a component's for three, say, or keep an
  outpost's for a month while chasing an outpost problem.

Three things are never removed, however old:

- a declared value (an operator's assertion is the truth, not a sample);
- the latest property value of every series, so a value that has not changed in months still shows
  (a metric is measured every check, so a metric series still collected always has a recent
  reading; one past its window shows no reading within retention);
- the value a command set out to write, which its history reads back.

A change to a window takes effect at the next pass.

A metric or property type can keep its own window, overriding its lane's: keep a busy probe metric
for three days, say, or a property that matters for a year. Shipped types included, though they are
otherwise read-only, since this is your install's choice and an upgrade never resets it:

```sh
omniglass metric-type setRetention tcp-connect-time --days 3
omniglass property-type setRetention endpoint-reachable --days 365
omniglass metric-type setRetention tcp-connect-time --days null    # back to metric_days
```

The type's window shows as `retention_days` on `omniglass metric-type get`. A property type keeps the
floor above (its latest value per series is never removed); a metric type keeps every reading inside
its window and none past it, so a series no longer measured shows no reading within retention. See
[storage](/architecture/storage/#retention-what-is-kept-and-for-how-long) for how it works.

:::design[Target design, tracked in #1159, #1339 and #1340]
Events, closed alarms, and the audit log each age out on a window of their own: events with a floor
that keeps any event an alarm still references (#1159), closed alarms after a retention window
(#1339), and the audit log on a policy that records its own prunes (#1340). Today all three are kept
indefinitely.
:::

## How often an endpoint reports on itself

Every endpoint reports on its own collection once a **report window**. This is built-in data: every
endpoint gets it, whatever its driver, with nothing to set up
([built-in and collected data](/guides/admin/drivers/#built-in-and-collected-data)):

- how long its polls took, how much CPU they cost the outpost, and how many failed;
- how many pushed messages its listeners claimed, and how many they did not;
- for a webhook endpoint, how many posts the server accepted and refused.

These show beside the device's own values on the component's page (`poll-duration`,
`push-message-count`, `webhook-post-count` and the rest), so a device that pushes and is never polled
still has telemetry. The window is `report_seconds` in the `collection` namespace: 60 by default,
from 10 to 3,600. A shorter window shows a stall sooner and costs more rows; each endpoint lands a few
rows per window, however fast its driver polls. A change nudges every outpost to pull its worklist at
once, so it is in force within a second. See
[collection telemetry](/architecture/collection/#collection-telemetry-every-endpoints-account-of-itself)
for what each count means.
