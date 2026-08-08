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
available, with no need to enter edit mode: a red **Delete** for a group (a user instead has the escalating
[lifecycle](/guides/admin/users/) of **Disable**, then **Archive**, then **Purge**, so its left slot is
**Disable / Enable** and the stronger steps sit in the kebab), each behind a confirm. Secondary actions like **Impersonate** fold into a
**⋯** menu. A blade you may not write keeps the Edit and Delete pair in place, **greyed**, with the
reason on hover (or on keyboard focus): an **official** catalog row reads "Official: ships with
Omniglass and updates with it." on both buttons, while a missing permission greys **just that
button** and names the permission that would unlock it (`Requires <resource>:update` on Edit,
`Requires <resource>:delete` on Delete), so a verb you do hold stays live beside the one you lack.
A blade with no actions at all (a role) shows no bar.

The same bar carries **create**. A form that opens in a slide-over (New user, New tag, Upload
file) or as its own blade (New interface) puts its **Create** button in the bar at the foot of the
panel, never floating after the last field, and greys it out until the form is complete. **Cancel**
sits beside it on the forms that offer one; where it does not, the header **x** closes the panel.

## Create, edit, delete

- **New** opens a **draft** at the entity's own `/create` address (a form for display name, name,
  classifier, placement, and where applicable a parent). Identity is two fields you fill and one you
  never see: the **display name** is the friendly string a human reads ("HQ Boardroom DSP"), the
  **name** is the identifier the API, the CLI, and the URL carry (`hq-boardroom-dsp`, lowercase
  letters, digits, and hyphens, changeable later, see Edit), and the `id` is a uuid the platform mints
  and keeps internally. The name fills itself in from the display name until you edit it, so most of
  the time you type one field and check the other. **On a component, name is optional**: leave it
  blank and the platform mints one from the component's type and the next free number in its room
  (`display-1`, `display-2`, ...), a **Generated** badge marking it as the platform's to keep current.
  The classifier is the entity's shape: a component
  picks its [product](/guides/admin/products/), a system the
  [standard](/guides/admin/standards/) it conforms to, a location its
  [type](/guides/admin/location-types/). On a component and a system the classifier is **optional**, so a
  one-off unit or a system that matches no blueprint is legitimate; a location's type is
  required, since for a location the type is the only shape-definer. **Create** commits it and
  drops you straight into the new entity's detail in **edit mode**, so you can tag it and finish
  configuring in place instead of hunting for it back in the list. Bindings like tags need the
  entity to exist, so they unlock the moment it is created. On a location, the type you pick may
  restrict which parent types it can sit under (or require no parent at all); a placement outside
  that set is refused with a message naming both types, right on the create form.
- **Edit** (the pencil on a row, or the button in the detail) flips that same detail into edit
  mode: the fields become inputs and the tag editor goes live. The **Name** is editable here on a
  component, a system, and a location, with an inline **Check** button that reports whether a
  proposed name is valid and still free before you save. **Save** commits the changes, **Cancel**
  discards them. In **view** the detail is read-only, so tags and other bindings are shown but not
  editable until you enter edit.
- **Renaming is a separate act from an update, and separately granted.** It is its own call,
  `<resource>:rename` rather than `<resource>:update`, and **Save** patches the other fields first
  and sends the rename last: an operator holding update but not rename keeps the rest of the edit
  and leaves the name where it was ([ADR-0076](/architecture/decisions/)). A **group**'s name is
  read-only in the console and moves from the API or the CLI. Renaming changes the entity's URL, so
  bookmarks, runbooks, and integration config holding the old name stop resolving. Nothing inside
  Omniglass breaks: every reference holds the entity's `id`, so its tags, grants, alarms, and
  recorded history follow the rename, and the audit trail stays attributable across it. **Renaming a
  Generated component's name hands you the pen for good**: the platform stops tracking it, even
  through a later move or a product swap. A reset icon beside the name field (gated the same as
  rename) hands it back, regenerating from the component's current type and room and marking it
  Generated again.
- **Moving is a separate act from an update too, and separately granted**, the same split renaming
  draws. Placement (Parent on a component, a system, or a location; Location on a component or a
  system) is not a field on the regular edit save: it is its own call, `<resource>:move` rather than
  `<resource>:update`. An operator holding update but not move can still edit the label, type, or
  product, but the placement fields stay read-only. Where the console offers a live picker, **Save**
  patches the other fields first and sends the move second, so a refused move (a cycle, a
  placement-type mismatch, or a taken name at the destination, naming both parties) leaves the rest of
  the edit in place rather than undoing it.
- A **location**'s edit mode makes its **Parent** editable, the console's one live placement picker
  today (with the move permission): the Placement section swaps its read-only fact for a picker
  narrowed to the location type's allowed parents (or, when unconstrained, every location), excluding
  the location's own subtree. Moving back to root is not offered here, and the platform has no
  clear-to-root capability for a location at all, not even from the API or CLI.
- A **component**'s and a **system**'s Placement (System, Location, Parent, and, for a component,
  Product) is **read-only in the console today**, in view and in edit alike: you set them at create
  and re-home an entity through the API or CLI (`omniglass component move`, `omniglass system move`).
  Both are movable via `:move` the same way a location's Parent is (an empty location unplaces a
  component or a system; clearing parent to lift an entity to a root needs an all-scoped grant, the
  same authorization creating a root entity already requires, since a scope-limited operator clearing
  parent would otherwise walk an entity out of every subtree their own grant was ever limited to; a
  re-parent that would put an entity under itself or one of its own children is refused); the console
  just has no picker for either yet. From the CLI, an entity's `<name>` argument takes a uuid, a bare
  name, or its full **dotted address** (`boi.17c.415a.$comp.display-1`), the location path down to a
  `$comp`/`$sys` accessor and the entity's own place from there. **Quote it**: an unquoted `$comp`
  disappears before the shell ever sends the request, so `omniglass component get
  'boi.17c.415a.$comp.display-1'` (single quotes), never bare.
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

**An assignment can be refused, and the refusal tells you why.** A role's [accepted types and pinned
products](/guides/admin/standards/#roles-what-a-conforming-system-needs-filled) are the typed-slot
guard: assign a component of the wrong type and you get both parties named
(`component "panel-1" is a display; role "Table microphone" wants a video-bar`), which is either a
sign you picked the wrong component or that the role needs widening.

Declaring the roles is on the [Standards guide](/guides/admin/standards/#roles-what-a-conforming-system-needs-filled);
this panel is where they get staffed.

## Alarms on a component

An **alarm** says what is wrong with **one component**. The component's **Alarms** panel lists the
active ones newest first, with a **Recently cleared** group beneath them: what is wrong now on top,
what was wrong underneath.

Raising one takes two things:

- a **severity**: `info`, `warning`, or `critical`. This is how loudly to treat it, and it sets the
  **component's own** verdict (any active alarm makes the component degraded, a critical one an
  outage). Only an **outage** stops a component occupying a role it fills: an info or warning
  alarm still degrades it, but the component keeps its slot, so a quiet issue does not short-staff
  a room on its own;
- a **message**, for whoever reads it later. Write it for the person who finds this at 8am, not for you.

**Clearing keeps the row.** The alarm moves to the history with the time it was cleared, so what was wrong
and when survives the fix. Clearing one twice is a plain miss rather than a silent success.

Both writes take effect immediately and completely: the room's verdict, the location above it, and the
recorded history all move in the same transaction as the alarm. There is no wait and no refresh cycle.

From the CLI: `omniglass component alarm list <name> [--include-cleared]`,
`omniglass component alarm create <name> --severity <level> --message <text>`, and
`omniglass component alarm delete <name> <id>`.

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
  -> mic-pod-2 is down (its own verdict, from the alarm)
    -> role room-mic wants 2, and mic-pod-2 no longer occupies its slot
      -> only 1 assigned component still occupies it
        -> role impaired, impact degraded
          -> hq-r1 is degraded
```

Read it bottom-up when you want the verdict and top-down when you want the fix. A role can also be
impaired with **no down component named**, which means it is **short-staffed** rather than broken:
nobody is assigned. Those are two different jobs, and the panel keeps them apart.

**The History strip is the answer to "since when".** It is the same shape as the reachability
availability strip: one segment per stretch the entity held a verdict, drawn from the **recorded edges**
over the last 30 days. It is not a sample and not a redraw of what somebody happened to look at; each edge
was written at the moment the estate changed, by the write that changed it. That is what makes "it broke
Friday at 18:40 and came back Monday at 09:15" answerable on Tuesday.

From the CLI: `omniglass system health list <name>` and `omniglass location health list <name>`.

## The whole loop, end to end

Once, in order, on a real room:

1. **Declare the roles with their impact.** On the room's
   [standard](/guides/admin/standards/#roles-what-a-conforming-system-needs-filled), give **Main Display**
   impact **outage** and **Room Microphone** impact **degraded** with quorum 2, accepting `video-bar`.
   Every conforming room inherits both immediately.
2. **Staff the system.** Assign components to each role from the system's **Roles** panel. A component
   of the wrong type is refused by name (`component "panel-1" is a display; role "Room Microphone"
   wants a video-bar`), so a wrong assignment never becomes a wrong verdict.
3. **Raise an alarm.** On one of the mic pods, raise a `critical` alarm.
4. **Watch the room move.** The system goes **degraded** (the `room-mic` role now has one occupant
   against a quorum of 2, and its impact is `degraded`), and the location above it follows. Had
   the alarm been on the main display instead, the room would be an **outage**, because that role says so.
5. **Read the Health panel** to find the cause: the impaired role, the component that went down, and the
   alarm that took it down, with its message and the time it was raised. Walk to the pod.
6. **Clear the alarm** once it is fixed. The room returns to **healthy** in the same transaction, and the
   alarm row stays in the component's history.
7. **Read the history afterwards.** The transition strip now shows the exact stretch the room was
   degraded, with the edge at the moment the alarm went up rather than the moment you opened this page.
   That is the whole point: come back in three weeks and the answer is still exact.
