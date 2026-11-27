---
title: Your first ten minutes
description: "Bring up a working Omniglass on Docker with a simulated fleet, then break a device in the console in ten steps and watch the platform notice."
sidebar:
  order: 3
  badge:
    text: Built
    variant: success
---

A **lab** is a working Omniglass on your own machine: the real server and console, its own Postgres,
and an outpost collecting from a fleet of simulated devices. It needs only Docker (Linux, macOS or
Windows). This page brings one up and walks one example through the console: you break a device and
watch the platform notice, raise an alarm, and recover.

## Bring it up

From a checkout:

```bash
git clone https://github.com/hyperscaleav/omniglass.git && cd omniglass
make
./bin/omniglass lab run
```

With a [release binary](/deployment/install/#download-a-binary) there is nothing to build: run
`omniglass lab run`, which pulls its own release's image by the digest stamped in at release. The first run takes a few minutes; later ones, under one. It prints one line per
stage, then where to go:

```text
Your omniglass lab is ready (it took 14s).

  Console    http://127.0.0.1:8080/web
  Sign in    user dev, password dev

  Stop it    press Ctrl-C here. Your lab's data is kept for next time;
             omniglass lab run --fresh starts over from the seed.
```

What each stage means and what "ready" checks is in [the lab](/developer/lab/#when-the-lab-is-ready).

## Break a device, in ten steps

Everything here is in the console. The fleet is the dev fleet; the device is Touch Panel 2 in Media
Lab, which serves the system Media Lab Teaching 2.

1. **Sign in.** Open the console address and sign in as `dev`, password `dev`. You land on **Home**.
   Every screen carries a **Lab instance** banner, so a lab is never mistaken for a real deployment.
2. **Read Home.** The **Need attention** card counts the systems and components in outage, degraded
   or incomplete. Note the number; the lab plants a few faults, so it is not zero.
3. **Open Simulation.** Choose **Simulation** in the sidebar. It lists every simulated device the
   outpost collects from.
4. **Find the panel.** Type `panel-2` in the filter. One row is left.
5. **Unplug it.** Open the row's **Break** menu and choose **Unplug**. Nothing to type: the menu is
   built from what this device can do.
6. **Read the fault chip.** The row now carries an **unplugged** chip with how long it has held. The
   panel has gone dark on every protocol, as if its cable were pulled.
7. **Open the panel.** Press Ctrl-K, type `panel-2` and press Enter. Touch Panel 2's page shows the
   same chip beside its health. It still reads healthy: the reachability rule waits for the failure to
   hold 15 seconds, so one missed probe never pages anyone.
8. **Watch the alarm open.** Within about a minute the page shows a **High** alarm on the panel and
   its health changes.
9. **Follow it to the system.** Under the systems the panel serves, open **Media Lab Teaching 2**.
   **Why** names the alarm, the **Touch Control** slot reads short, and the system is degraded. Home's
   **Need attention** count has gone up too.
10. **Fix it.** Back on the panel's page, click the **x** on the fault chip. The panel is plugged back
    in; on the next reading the alarm clears, and the system and Home go back to how they were.

That is the whole loop: collect, judge, alarm, recover. The model behind it is
[the model in one pass](/start/the-model/); the rule that fired is in
[alarms and health](/operator/alarms/).

## Keep going

- **More guided walkthroughs**, each a recording of a real console flow on this same lab:
  [the list](/start/walkthroughs/).
- **Faults already planted for you**, such as a rotated credential or a reimaged device's changed key,
  and how to break any device yourself: [planted faults](/developer/lab/#planted-faults).
- **Stopping the lab and keeping your data**: press Ctrl-C. The lab removes its containers and keeps
  its data for next time; `omniglass lab run --fresh` starts over from the seed
  ([the lab](/developer/lab/#stopping-it-and-your-data)).
