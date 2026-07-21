---
title: Work with an entity
description: "Opening an entity's blade, drilling into its children, and creating, editing, or deleting through the footer action bar."
screenshots:
  - id: entity-blade
    path: /web/locations
    alt: "A location's blade slides in from the right with its details and a footer action bar."
    steps:
      - action: click
        selector: "text=East Campus"
---

Once you have [found an entity](/guides/operator/inventory/), you open it, read it, and change
it the same way everywhere in the console.

## Open an entity

Click a row to open its **blade**, a panel that slides in from the right with the entity's
details. From a blade you can drill into a child (it stacks another blade behind the first),
step back with the breadcrumb, or **Maximize** to the full detail page. The full page has its
own URL, so it is shareable and bookmarkable; a blade is a quick look that does not change the
URL. Rows are keyboard-operable: Tab to a row and press Enter to open it.

::screenshot{#entity-blade}

The identity pages (Users, Groups, and Roles) use the same blade, and there drilling crosses entities: from a
user you open a group's blade over it, and from a group you open a member's user blade, each stacking so you can
trace where access comes from without leaving the page. Each page roots one entity and drills one direction (a
user's groups, a group's members), so the stack stays shallow and the reverse relation on the far blade is a
read-only reference.

## Edit through the footer action bar

A detail blade opens **read-only**, and every entity is edited the same way through the **footer action bar**.
The blade header is chrome only (back, full-page, close); the actions live in the bar at the foot of the blade.
**Edit** (right) opens edit mode: the profile becomes inputs, the members and grants go live, and the right
cluster swaps to **Cancel** and **Save**. Changes stage locally so you can check your work first; **Save**
commits them together, **Cancel** discards them. The **destructive** action sits on the **left** and is always
available, with no need to enter edit mode: a red **Delete** for a group (a user is disabled, never deleted, so a
user's is **Disable / Enable**), each behind a confirm. Secondary actions like **Impersonate** fold into a
**⋯** menu. Edit appears only if your grants allow it, and a read-only blade (a role) shows no bar at all.

## Create, edit, delete

- **New** opens a **draft** at the entity's own `/create` address (a form for name, classifier,
  placement, and where applicable a parent). The classifier is the entity's shape: a component
  picks its [product](/guides/admin/products/), a system the
  [standard](/guides/admin/standards/) it conforms to, a location its
  [type](/guides/admin/types/). On a component and a system the classifier is **optional**, so a
  one-off unit or a system that matches no blueprint is legitimate; a location's type is
  required, since for a location the type is the only shape-definer. The name is the entity's address: lowercase
  letters, digits, and hyphens (it can be changed later, see Edit). **Create** commits it and
  drops you straight into the new entity's detail in **edit mode**, so you can tag it and finish
  configuring in place instead of hunting for it back in the list. Bindings like tags need the
  entity to exist, so they unlock the moment it is created. On a location, the type you pick may
  restrict which parent types it can sit under (or require no parent at all); a placement outside
  that set is refused with a message naming both types, right on the create form.
- **Edit** (the pencil on a row, or the button in the detail) flips that same detail into edit
  mode: the fields become inputs and the tag editor goes live. The **technical name** (the
  address) is editable here too, with an inline **Check** button that reports whether a proposed
  name is a valid slug and still free before you save; renaming changes the entity's URL, and
  existing links to the old name stop resolving. **Save** commits the changes, **Cancel** discards
  them. In **view** the detail is read-only, so tags and other bindings are shown but not editable
  until you enter edit.
- A **location**'s edit mode also makes its **Parent** editable: the Placement section swaps its
  read-only fact for a picker narrowed to the location type's allowed parents (or, when
  unconstrained, every location), excluding the location's own subtree. Moving back to root is
  not offered; a move a stale picker still lets through is refused the same way as create, inline,
  naming both types.
- **Delete** removes it, with a confirm. These actions appear only if your grants allow them.

## Properties on the detail

A component, a system, and a location each carry a **Properties** panel on their detail: one row per
property their classifier declares, resolved to the value set here or the classifier's default.
Overrides are staged with the rest of the edit and committed by the same **Save changes**. It is one
surface over one resolver, so the panel reads the same on all three; the full walkthrough is in the
[Properties guide](/guides/admin/properties/#set-a-property-on-an-instance).

## Roles on a system

A **system** carries one more panel: **Roles**, the slots it needs filled. A role is a slot (a room
microphone, a main display), not a component, so the room can say what it needs before anything is
assigned and an **empty slot stays visible**. These are slots in a room, not the
[roles that grant people access](/guides/admin/access/); the two share only the word.

Each row is one role with **where it came from**, **who fills it**, and **how many more it wants**:

- **Inherited or declared here.** A role marked as coming from the standard is declared on the
  [standard](/guides/admin/standards/) this system conforms to, and every conforming system has it.
  A role declared on this system is this room's own. A **one-off system** (conforming to no standard)
  has only its own.
- **Assigned and understaffed.** A role has a **quorum**, how many components should fill it. Two
  assigned against a quorum of two reads as staffed; one reads as short by one. That is true the
  moment you enter it, with nothing collecting: staffing is a fact about your model, not a
  measurement.
- **Impact.** Each role also says what the room loses when the slot is not being filled properly:
  **outage**, **degraded**, or **none**. That is what turns a broken component into a room-level verdict
  further down this page, and it is declared on the
  [standard](/guides/admin/standards/#roles-what-a-conforming-system-needs-filled) or on the system.
- **Assign** picks a component to fill the role; **unassign** takes it out and the role goes back to
  understaffed. Assigning the same component twice changes nothing.
- **A component staffing a role cannot be deleted.** Unassign it first. The refusal is deliberate: a
  delete that silently emptied a slot would leave the room quietly wrong.

**An assignment can be refused, and the refusal tells you why.** A role requires a set of
[capabilities](/guides/admin/capabilities/), and a component must provide **every** one of them.
Assign one that does not and you get the gap by name (`missing microphone, speaker`), which is either
a fix on the component's **Capabilities** panel or a sign that it is the wrong component for the slot.

Declaring the roles is on the [Standards guide](/guides/admin/standards/#roles-what-a-conforming-system-needs-filled);
this panel is where they get staffed.

## Capabilities on a component

A **component** carries a **Capabilities** panel: what it actually provides, resolved from its
[product](/guides/admin/products/) plus what this unit adds and minus what it suppresses. It is the set
every role assignment is checked against, and it is how a component with **no product** provides
anything at all. The walkthrough is in the
[Capabilities guide](/guides/admin/capabilities/#what-a-component-actually-provides).

## Alarms on a component

An **alarm** says what is wrong with **one component**, and which of its capabilities the problem takes
away. The component's **Alarms** panel lists the active ones newest first, with a **Recently cleared**
group beneath them: what is wrong now on top, what was wrong underneath.

Raising one takes three things:

- a **severity**: `info`, `warning`, or `critical`. This is how loudly to treat it, and it sets the
  **component's own** state (any active alarm makes the component degraded, a critical one an outage);
- a **message**, for whoever reads it later. Write it for the person who finds this at 8am, not for you;
- the **capabilities it degrades**. This is the one that matters beyond the device. A component keeps its
  capabilities on paper, but a degraded one **does not count** toward any role that requires it. An alarm
  that degrades nothing is a note on the device and reaches no room, which is often exactly right.

**Clearing keeps the row.** The alarm moves to the history with the time it was cleared, so what was wrong
and when survives the fix. Clearing one twice is a plain miss rather than a silent success.

Both writes take effect immediately and completely: the room's verdict, the location above it, and the
recorded history all move in the same transaction as the alarm. There is no wait and no refresh cycle.

From the CLI: `omniglass component alarms <name> [--include-cleared]`,
`omniglass component raise-alarm <name> --severity <level> --message <text> --capabilities <ids>`, and
`omniglass component clear-alarm <name> <id>`.

## Health on a system or location

A **system** and a **location** each carry a **health verdict**, shown as a badge on the detail and in
the systems list:

| verdict | means |
|---|---|
| **healthy** | nothing the room depends on is impaired |
| **degraded** | it is working, worse |
| **outage** | it is not working |

A location's verdict is the **worst** of every system placed anywhere beneath it, so a campus reads red
when one room in one building is out. A system's verdict is the worst contribution among the **roles** it
needs filled.

**The Health panel is the answer to "why".** A bare "degraded" gives you nothing to do, so the panel
names the whole chain instead, role by role:

```text
alarm on mic-pod-2 (critical, "no audio on channel 1")
  -> degrades: microphone
    -> role room-mic requires microphone, and wants 2
      -> only 1 assigned component can currently fill it
        -> role impaired, impact degraded
          -> hq-r1 is degraded
```

Read it bottom-up when you want the verdict and top-down when you want the fix. A role can also be
impaired with **no alarm named**, which means it is **short-staffed** rather than broken: nobody is
assigned, or what is assigned never provided what the role requires. Those are two different jobs, and
the panel keeps them apart.

**The History strip is the answer to "since when".** It is the same shape as the reachability
availability strip: one segment per stretch the entity held a verdict, drawn from the **recorded edges**
over the last 30 days. It is not a sample and not a redraw of what somebody happened to look at; each edge
was written at the moment the estate changed, by the write that changed it. That is what makes "it broke
Friday at 18:40 and came back Monday at 09:15" answerable on Tuesday.

From the CLI: `omniglass system health <name>` and `omniglass location health <name>`.

## The whole loop, end to end

Once, in order, on a real room:

1. **Declare the roles with their impact.** On the room's
   [standard](/guides/admin/standards/#roles-what-a-conforming-system-needs-filled), give **Main Display**
   impact **outage** and **Room Microphone** impact **degraded** with quorum 2. Every conforming room
   inherits both immediately.
2. **Staff the system.** Assign components to each role from the system's **Roles** panel. A component
   that cannot fill the role is refused by name (`missing microphone, speaker`), so a wrong assignment
   never becomes a wrong verdict.
3. **Raise an alarm.** On one of the mic pods, raise a `critical` alarm degrading `microphone`.
4. **Watch the room move.** The system goes **degraded** (the `room-mic` role now has one satisfying
   component against a quorum of 2, and its impact is `degraded`), and the location above it follows. Had
   the alarm been on the main display instead, the room would be an **outage**, because that role says so.
5. **Read the Health panel** to find the cause: the impaired role, the capability it lost, and the alarm
   that took it, with its message and the time it was raised. Walk to the pod.
6. **Clear the alarm** once it is fixed. The room returns to **healthy** in the same transaction, and the
   alarm row stays in the component's history.
7. **Read the history afterwards.** The transition strip now shows the exact stretch the room was
   degraded, with the edge at the moment the alarm went up rather than the moment you opened this page.
   That is the whole point: come back in three weeks and the answer is still exact.
