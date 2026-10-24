---
title: "A create that writes a membership costs what the membership route costs"
date: 2026-08-12
refs: []
form: bullet
---

- **A create that writes a membership costs what the membership route costs.** `POST /components`
  accepts a `system` and inserts that component's primary membership from it, the same row the
  membership route writes under `system:update`, while the create asked for no system permission at
  all. The gate on the membership route was therefore decorative: the create was the cheaper way
  around it. The create now requires `system:update` when the reference is present, and resolves it in
  that scope rather than in the read scope the location beside it uses, because what decides the
  action a placement reference resolves for is what the write DOES with it.

  The consequence was accepted rather than worked around: `operator` holds `component:create` and no
  system permission of any kind, so an operator can no longer create a component into a system. That
  is the line the seeded roles already draw (an operator maintains components, a deploy tech builds
  out systems and their membership), and granting `operator` the permission would have handed it
  general system editing to buy one binding.

  A narrowing discovered as a 403 after a form is filled in is the outcome the do-not-offer-what-the-
  platform-refuses rule exists to prevent, so the console does not offer the system picker to a
  principal that cannot use it: the slot keeps its place in the placement grid and explains itself,
  naming the permission to ask for. The API's refusal names it too, which is what the CLI prints and
  what its generated help now says.

  Two paths write one row, so the spec has to say so. The second permission is conditional on the
  request rather than on a tier, and middleware cannot see a body, so the check runs in the handler
  and the permission is published as `x-omniglass-conditional-permission` beside the route's primary
  stamp, the same shape a platform-tier write already used. It joins the route-derived permission
  universe, so the roles view and the docs lint both see it without being told separately.
