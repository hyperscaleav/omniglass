---
title: Operator
description: "Running the fleet day to day: finding things, reading health, acting on alarms, and doing it from the console or the CLI."
sidebar:
  label: Overview
  order: 0
---

**For:** the people who watch and run the fleet: the AV technician, the support desk, the engineer on
call.

**In this section:** signing in; finding a location, system or component and reading its view;
creating and editing what is in the fleet; alarms, health and commands; your inbox; starting from a
template; connecting outposts and reading reachability; whether Omniglass itself is well; and a
simulated fleet to practise on. Who may sign in and what they may do is the [Admin](/admin/)
section's job, and standing the platform up is [Deployment](/guides/deployment/).

## Two ways in, one API

- **The console**, served by the binary at `/web`, is the point-and-click surface these pages walk
  through.
- **The CLI** is the `omniglass` binary acting as a client of a running server, for the terminal and
  for scripts. The [CLI guide](/guides/cli/) covers signing it in; every command is in the
  [CLI reference](/reference/cli/).

Both call the same API, so both pass the same permission and scope checks, and anything you can do in
one you can do in the other.

## The pages

- **[Sign in and your profile](/operator/sign-in/)**: a password or a bearer token, your label and
  picture, your password, sessions and tokens.
- **[Explore the fleet](/operator/fleet/)**: the outline of every location, the chip filter, and the
  view each system and location opens.
- **[Create and edit](/operator/entities/)**: the side panel, the edit face, names and labels,
  properties, and a system's slots.
- **[Alarms and health](/operator/alarms/)**: an alarm's life (raise, acknowledge, snooze, clear), how
  health rolls up from it, and the loop end to end.
- **[Commands](/operator/commands/)**: sending an instruction to a component and reading its outcome.
- **[Your inbox](/operator/notifications/)**: the alarms you are told about, and subscribing yourself
  to more.
- **[Start from a template](/operator/templates/)**: the shipped templates and what starting from one
  creates.
- **[Outposts and reachability](/operator/collection/)**: enrolling an outpost, adding an endpoint,
  and reading why a device does or does not answer.
- **[Platform health](/operator/platform-health/)**: whether Omniglass itself is well, and where work
  is lost.
- **[Simulate a fleet](/operator/simulation/)**: simulated devices behind a real outpost, to exercise
  and break on purpose.

## Getting around

- **The sidebar** has one door into the fleet, **Explore**, then **Outposts**, and sections grouped
  under Values, Catalog and Admin. **Catalog** is a single entry: its own subrail groups the catalogs
  by the fleet noun they serve (Telemetry, Actions, Components, Systems, Locations, Metadata), and each
  catalog opens at its own address. These pages name a catalog by group and entry: **Catalog, under
  Locations: Types**. A catalog whose backend has not landed is dimmed with a **soon** tag. The
  subrail's first entry, **Overview**, has a card per group with live counts.
- **The top bar** names the section you are in and carries **Search (⌘K)** and, if you may use the
  assistant, **Ask omnibot**, which opens [omnibot](/guides/agents/#omnibot-in-the-console) with the
  page you are on as its context. The console is dark-only; its theme is an
  [Admin > Settings](/admin/settings/) value, not a per-session toggle.
- **⌘K** (or Ctrl-K) is the command palette, a jump to any destination by name: arrow keys move,
  Enter goes, Esc closes. It finds each catalog by its own name (`products` lands on **Catalog, under
  Components: Products**) and every entry under a group by the group's name (`telemetry` lists
  Metrics, Properties and Events). Its last row hands what you typed to omnibot as a draft. The
  palette jumps between pages; finding one thing in the fleet is Explore's own
  [filter](/operator/fleet/#finding-one-thing).
- **You see only what you can use.** A tab you hold no read permission for is hidden, an action you
  may not take does not render, and an address you may not read redirects you to Home. The server
  checks every request regardless; the console only hides what it already knows you cannot reach.

## What you see is your scope

Every read is filtered to **your scope** on the server. An operator granted one location sees that
location and everything under it, on every page, in every list, in the CLI, with nothing to configure:
scope follows your [grants](/admin/access/). The **Access** view in [your
profile](/operator/sign-in/#your-profile) shows the roles you hold and the permissions they carry.
The model behind both layers is [identity and access](/architecture/identity-access/).

How the console is built is the [UI architecture](/architecture/ui/); how to add to it is the
[design system](/contributing/design-system/).
