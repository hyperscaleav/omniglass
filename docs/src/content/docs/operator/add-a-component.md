---
title: Add a component
description: "Adding a device in one flow: its product and name, the system it serves and its location, its endpoint and driver, the credential it accepts, a test that reads its first values, and its own page."
sidebar:
  label: Add a component
  order: 3
  badge:
    text: Built
    variant: success
walkthroughs:
  - id: add-a-component
---

Adding a device is one flow that ends with data. **Add a component** walks six steps on one page:
what it is, where it sits, how the outpost reaches it, the credential it accepts, a test that reads
it now, and the component's own page with its first values. Each step is a piece of the console you
also meet elsewhere (the create form's naming, the placement pickers, a driver's inputs, the secret
form, an endpoint's poll test), so what you learn here is what you read later. The model behind the
pieces is [the model in one pass](/start/the-model/) and [data collection](/architecture/collection/).

::walkthrough{#add-a-component}

## Where it starts

- **Explore**, **New**, **Component** (or a location row's **+**, which starts it at that location).
- A **system's** page: **Add a component** beside its Components, which starts it in that system.
- In a lab, the **Simulation** page: **Add and monitor a device**, which starts it on a simulated
  device (below).

It needs `component:create` and `endpoint:create`; joining a system also needs `system:update` on it.
The plain create form, which creates the component alone, stays one link away as **Use the plain
form (advanced)**.

## The steps

1. **Product.** Type a product's name, its type or its vendor; each choice names its type and vendor
   so two lookalikes tell apart. The product decides the component's type, the driver it suggests
   and the name the platform gives it: the name and label fields work as they do on the plain form
   (locked on the platform's answer, unlocked to type your own).
2. **Where.** The **system** it serves, the **slot** it fills there (only the slots a component of
   this product may fill are offered), and its **location**. Choosing a system sets the location to the
   system's own. The flow sets it, since the platform does not yet inherit a component's location from
   its system ([#878](https://github.com/hyperscaleav/omniglass/issues/878)); you can still pick
   another.
3. **Endpoint.** The **driver** is the product's own, chosen for you, with the drivers other
   products of its type use listed after it (**Show every driver** lists the rest). The **outpost**
   is chosen when there is only one. Then the driver's parameters, such as the device's address.
4. **Credentials.** Each secret the driver needs is **picked** from the secrets of that kind you can
   read, never typed by name. A missing one is created in place: type its label and choose
   **Create a secret**, or **New secret**, and the secret form opens with the kind already set. The
   endpoint keeps the secret's name; its value stays sealed on the server.
5. **Test.** **Create and test** creates the component and its endpoint, then the outpost runs each
   of the driver's polls at once and shows every value it read, or the real reason it could not. A
   refused login points you back to **Credentials**; anything else (no answer, the wrong port, an
   outpost not connected) back to **Endpoint**. Going back takes the component and its endpoint out
   again, so the next try starts clean.
6. **Done.** **Open the component** lands on its Overview, where the values the test read are
   already collected. If the device is not answering yet and you want to keep it anyway, **Keep it
   untested** does the same.

Nothing is created before **Create and test**. **Cancel**, **Back** from the test, or leaving the
page before the last step removes the component and endpoint the flow created, so an abandoned add
leaves nothing behind.

## A simulated device

In a lab (or beside any outpost running the simulator) the **Endpoint** step offers **Use a
simulated device**: the server plans a new simulated device of the chosen product, at the next free
address in the simulated block, and the flow fills in the outpost beside the simulator, that address
and the driver's inputs, including the platform secret the simulator answers to. Nothing has to be
read off the Simulation page. The simulator starts answering at that address once the endpoint
exists. The same step lists the simulated devices of that product that already answer, each a
component of its own, with its address.

The plan reserves nothing: another add in between can take the same address. From the CLI or the
API the plan is `omniglass simulation plan-component <outpost> --product <product>`
(`GET /outposts/{name}/simulation/components:plan`); see [simulation](/operator/simulation/).
