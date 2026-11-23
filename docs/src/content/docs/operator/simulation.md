---
title: Simulate a fleet
description: "Running the simulator beside an outpost, watching the collector reach its simulated components, exercising one's API, adding one, and breaking one on purpose to watch monitoring react."
sidebar:
  label: Simulate a fleet
  order: 10
  badge:
    text: Built
    variant: success
walkthroughs:
  - id: sample-simulation
---

Omniglass can simulate the components it monitors. The simulator ships in the same binary: beside a
collection outpost it serves every component placed on that outpost whose product it has a profile for,
each on its own loopback address, speaking the protocols the real product speaks. The outpost collects
from those simulated components exactly as it would from hardware, so the whole monitoring loop runs with no gear
at all, and an operator can break one on purpose to see what monitoring makes of it. The model
behind it is [simulation](/architecture/simulation/); every action here passes the same
[scope](/operator/#what-you-see-is-your-scope) and permission checks as the rest of the
console.

## Run it

**In a lab**, with nothing but Docker, run `omniglass lab run` (`make lab` from a checkout): its own
server and database, and the outpost `edge-hq` running the simulator in its own process with impairments
applied, so the seeded fleet is collected for real and the console is at `http://127.0.0.1:8080/web`
([your first ten minutes](/start/first-ten-minutes/)).

**Beside an outpost of your own**, run the outpost with the simulator in its process:
`omniglass outpost run --simulate` serves the components placed on that outpost on its own loopback, with
the outpost's own identity ([run a simulator](/deployment/simulator/)).

**As a process of its own**, the simulator is `omniglass sim run`, started beside an outpost with that
outpost's identity:

```sh
omniglass sim run --server https://omniglass.example --name edge-hq --token "$OUTPOST_TOKEN"
```

It reads the same `OMNIGLASS_SERVER`, `OMNIGLASS_OUTPOST_NAME` and `OMNIGLASS_OUTPOST_TOKEN` as
`outpost run`, and must share the outpost's loopback (the same host, or the same pod or container network
namespace). It binds the profiles' real ports when it may (SNMP 161, HTTP 80, SSH 22), or lifts them
with `--port-offset` when it may not; the endpoints must target the ports it serves. The privileges
it can use, and how to grant them, are in the [deployment guide](/deployment/simulator/).

The simulator follows the fleet as it changes: every ten seconds it pulls the components placed on
its outpost, so a component added on the server starts answering, and a deleted one goes dark, with no
restart.

## See it

While a simulator is connected, **Simulation** appears in the sidebar (with `simulation:read`, which
viewers hold). You see each simulator that serves a component in your scope, and only those
components: a technician scoped to one location sees the simulated components under it. The page has a card per outpost with a simulator, saying whether it is connected, its total
hits and components, what it may do where it runs (**Low ports**: bind ports below 1024; **Port offset**:
the lift it applies when it cannot; **NET_ADMIN**), and whether it can apply impairments (and if not,
why). Under it, a table lists every simulated component with its product, address, status, hits and
last hit. The **status** says what the simulator is doing with it: **running**; **starting** (added since
the simulator's last pull, so it begins within about ten seconds); **failed** (handed to the simulator,
which could not start it: a port it cannot bind, or an address that is not loopback); or **offline** (the
simulator stopped reporting). Anything you broke on it shows as a chip beside the status
(**unplugged**, or the delay and loss you set), with a note when the simulator could not apply it, and a row **lights up** each time the collector reaches the component: a
poll, a connect, a ping. A row that never lights up is a component nothing is collecting from.
When no simulator is connected the entry is hidden, since there is nothing to show.

::walkthrough{#sample-simulation}

From a terminal the same view is `omniglass simulation get`, and one outpost's is
`omniglass outpost simulation <outpost>`.

A simulated component's own page carries a **Simulation** section (its outpost, address, hits, and
anything broken on it), so the component and its simulation are one page. **Open simulation** opens the same
simulation drawer a row on the Simulation page opens.

## Exercise a component

The drawer lists the component's whole **API surface**, read from its profile, as **Operations**: each
protocol surface it serves (its protocol and the port the simulator binds) is a group, and each HTTP route,
command and SNMP OID it answers is one row. A row is a disclosure: it opens in place to its target, the
state paths a route writes, the request (edit any of it), a **Send** button (with `simulation:update`, which
operators hold) and, beneath it, the answer: the HTTP status and body, a command's response, an OID's
value. One row is open at a time and nothing scrolls. The last row of each surface, **send your own
request**, takes a request no profile lists.

The simulator answers from the component's live state, so firing a command that changes it (a panel's
`SET INPUT hdmi2`) changes what the collector reads on its next poll. A fire counts as a hit, so the
row lights up. It reaches the component even when it is unplugged, so a broken one can still be
inspected. Every fire is audited.

```sh
omniglass component simulation get panel-1                                   # the surface
omniglass component simulation fire panel-1 --surface nvp --command "GET INPUT"   # the answer
```

## Add a component

**Add simulated component** on the Simulation page (with `simulation:update`, `component:create` and
`endpoint:create`) asks for an outpost with a simulator, a product the simulator has a profile for (type
part of the product or its profile to narrow the list; each match shows the profile under it), a name,
and optionally a location. It creates the component and the
endpoint the outpost reaches it through, at the next free address in `127.60.0.0/16`; the simulator
serves it from its next pull. From a terminal:

```sh
omniglass simulation profiles                                               # the products it can simulate
omniglass simulation add-component edge-hq --name panel-9 --product newtron-panel-7
```

In the drawer, **Product** and **Change** move a component among the products its profile simulates (a
display line in its sizes, say), which keeps its endpoint valid (with `component:update`), and
**Remove** deletes the component after a confirmation, which removes it from the simulation (with
`component:delete`).

## Add a whole system

A simulated system is built to one of your **standards** (a meeting room, a huddle room, a digital
sign). Omniglass staffs each of the standard's slots with a simulated product that fits it, and lists
the slots it cannot fill yet, which stay short so the system honestly reads that way. On the
Simulation page, **Add simulated system** asks for an outpost, a standard, a system type, and optionally a
location and a name; choosing the standard previews its **plan** (each slot, the product it gets and how
many, and any short slot with its reason) before anything is created. From a terminal, preview the plan,
then add it:

```sh
omniglass simulation plan-system meeting-room
omniglass simulation add-system edge-hq --standard meeting-room --system-type meeting --location boardroom-a
```

The system and its components land together or not at all (with `simulation:update`, plus
`system:create`, `system:update`, `system:read`, `component:create`, `component:update`, `endpoint:create`
and `outpost:read`; `component:create` and `component:update` must reach the whole fleet, since the
components are created at the top level and each is then assigned its slot), named as any new system and
component are named, and the simulator serves them from its next pull.

## The drawer

A row opens the component's drawer: its **Faults** (the named actions and the chip, below) on top, then five
tabs, the last one chosen being remembered for the session.

- **Info**: where the simulator serves it, its status, hits and product, and its **State**, a form
  generated from its profile: each state path its API sets to a fixed list of values is a select showing
  what it holds now. Choosing one sends the request a named fault sends, so the form, the chip and the
  actions always agree.
- **Impairments**: unplug, latency, jitter, loss and rate, with **Repair** to clear them all.
- **Operations**: the API surface, below.
- **Behaviors**: what the profile changes on the device by itself over time (a temperature that drifts),
  read-only, or "No behaviors declared".
- **Events**: the device's own log of what reached it is not kept yet ([#2524](https://github.com/hyperscaleav/omniglass/issues/2524));
  the tab shows what the simulator does count, its hits and last hit. Terminal sessions on a line protocol
  are also not built yet ([#2525](https://github.com/hyperscaleav/omniglass/issues/2525)).

## Break it in one click

Every simulated component has a **Break** menu on its row and a **Faults** section in its drawer, with the
named ways to break it and **Restore** (with `simulation:update`, which operators hold; a viewer sees
what is broken and gets no controls). The actions are generated from what the component's profile can
do, so a display and a microphone get different menus:

- **Power off**, **Power on** and **Standby**, for a profile whose API sets its power.
- **Unplug** and **Replug**, the [impairment](#break-it-on-purpose) every simulated component has.
- A **quick set** for each other state path the profile's API sets to a fixed list of values: **Input:
  hdmi2**, **Mute**, **Shutter: closed**. The menu leaves out the value a path already holds.
- **Restore**, offered while anything is wrong: it repairs the impairment and sets every held path back
  to its profile default.

Nothing is typed: an action fires the profile's own route with the body it needs, the same request the
raw builder below would send. Power a seeded display off and the reachability rule opens its alarm on the
component once its sustain passes; **Restore** it and the next reading clears the alarm.

What is broken shows as a **fault chip** (its words, how long it has held, and an **x** that clears it) in the
**Faults** column of the row, in the drawer, and on the component's own page beside its health, where the
alarm it causes is. The chip is the simulator's own account, not the browser's: the simulator reports every
state path it holds off its default with each batch (and the impairment is stored), so a reload, or a
second operator's console, shows the same faults. The age counts from when the server first heard the
value, so a server restart starts it again. The **x** is **Restore**.

Below the faults, the drawer's **Operations** are the general path, for an operation no profile names as a
quick set (a command on a serial or SSH surface is one: quick sets are generated from HTTP routes only).

The list takes what you type: words with no `key:` narrow the rows as you type them (name, location,
product or address), and Enter keeps them as a chip beside the keyed filters (`status:failed`,
`fault:unplugged`). It shows fifty rows at a time (**Show more** adds fifty). The filter and the open
component are in the address (`?chips=` and `?c=`), so a copied link reproduces the view.

## Break it on purpose

The drawer's **Impairment** controls (with `simulation:update`) break the component the way real gear
breaks:

- **Unplugged**: dark on every protocol. Connects time out, pings go unanswered.
- **Latency** and **jitter**: a delay, with variation, on traffic toward the component. It shows in the
  collector's connect times.
- **Loss**: a share of packets toward the component dropped.
- **Rate**: a bandwidth limit on the component's link, both ways.

**Save impairment** sets the whole impairment at once (a field left empty is zero), and **Repair**
clears it.
The simulator applies it on its next pull, and the component's Reachability panel shows the collector
seeing it: unplug a component and its TCP and ping checks go down; plug it back and they recover.

Impairments apply only on a simulator started with `--impairments` and holding `NET_ADMIN`, since
they rewrite the firewall and traffic shaping of its network namespace. Without that the setting
still saves, and the outpost's card says why it is not applied. With it, **ping tells the truth**: an
address that no component answers on stops answering ping.

```sh
omniglass component impairment set panel-1 --unplugged       # dark on every protocol
omniglass component impairment set panel-1 --latency-ms 300  # slow (and plugged back in)
omniglass component impairment set panel-1                   # repaired
```

Unplugging closes the monitoring loop with nobody in between. Once the component's endpoint has
read `down` for the official reachability rule's fire sustain ([the shipped rules](/admin/rules/#the-rules-that-ship)),
the rule opens an alarm at its severity on it, the component goes to outage and stops filling its slots, and its system's health follows (degraded or
out, as the slot's impact says). **Catalog > Actions > Event rules** shows the rule's open alarm while it
lasts. Repair it and the next `up` reading clears the alarm and the health recovers. The
[rules guide](/admin/rules/) walks the same loop with a rule of your own.

Loss shows from the far end too. [Platform health](/operator/platform-health/#with-a-simulator)
sets what each simulated device saw beside what the outpost sent and the server collected: drop an snmp
device's packets and the outpost's **sent, not received** gap for snmp opens, read as impaired rather than
lost, while the device's received count falls; repair it and the attribution clears once the window
slides past the change.
