---
title: "A create that writes a membership costs what the membership route costs"
date: 2026-08-12
status: "Accepted"
summary: "`POST /components` accepts a `system` and INSERTS that system's primary membership from it, the same row `PUT /systems/{name}/members/{component}` writes under `system:update`, while the create asked for no system permission at all: the create was the cheap way around the membership route's gate. The create now requires `system:update` when the reference is present and resolves it in that scope, so **two paths writing one row cost one permission**. The accepted consequence is a live narrowing: `operator` holds `component:create` and no `system:*`, so an operator can no longer create a component INTO a system, which reads as the role line rather than collateral damage (an operator maintains components, a deploy tech builds out systems and their membership). Granting `operator` the permission was refused as a much larger grant than \"may bind a membership while creating\". A second permission conditional on the REQUEST is published like the platform tier's, as `x-omniglass-conditional-permission`, and enforced in the handler because middleware cannot see the body; the console hides the picker from a principal that cannot use it and the API's refusal names the permission, so the narrowing is met before the form is filled in. **Amended (#707 review):** the console gate read the PERMISSION only, so a principal holding `system:update` over an empty scope (a location-scoped `deploy` grant, since the cross-tier expansion is unbuilt, #10) was offered the picker and refused on submit, and the API answered \"system not found\" for a system that caller could `GET`; the gate now also requires a system carrying the scope-aware `update` action, and the bind takes `system:read` beside `system:update` so a readable row is refused by AUTHORITY (403) rather than by absence. **Amended (#713):** the residual this entry accepted is closed, the `:renderLabel` draft resolving the same reference through the create's own resolver and carrying its conditional permission, because a preview that resolved it in `system:read` alone both previewed a refusal and handed back the system's type label; the LOCATION reference stays `location:read` on both routes, read and rendered rather than written"
---

- **Date:** 2026-08-12 | **Status:** Accepted | **Pages:**
  [identity and access](/architecture/identity-access/), [API](/architecture/api/),
  [core entities](/architecture/core-entities/)
- **Context:** `POST /components` accepts a `system` reference, and what it does with it is insert the
  component's **primary membership** into `system_member`. That is the same row
  `PUT /systems/{name}/members/{component}` writes, and that route is gated on the `system:update`
  permission and resolves its system in the `system:update` scope. The create asked for neither: the
  reference resolved existence-only until
  [#700](https://github.com/hyperscaleav/omniglass/issues/700) made it resolve in `system:read`, and
  the route required `component:create` alone. A principal holding `component:create` and no system
  permission at all could therefore write a membership through the create that the membership route
  refused it, which makes the gate on the membership route decorative rather than binding.
- **Decision:** the create's `system` reference resolves in the caller's **`system:update` scope**,
  and the route requires the **`system:update` permission** when the reference is present. Two paths
  that write one row cost one permission. The scope half is not implied by the permission half and is
  the one a test has to drive separately: a principal holding `system:update` on one system and the
  all-scoped viewer floor beside it may READ every system in the estate, and before this could bind
  any of them on a create.
- **Decision (where the check lives):** in the handler, not the middleware, because the condition is a
  **body field** and Huma's operation middleware cannot see the body. To keep the generated spec a
  faithful map of what a route enforces rather than of what it always enforces, the second permission
  is **published** as an `x-omniglass-conditional-permission` extension beside the primary
  `x-omniglass-permission` stamp, exactly as a platform-tier write publishes
  `x-omniglass-platform-permission`, and it joins the route-derived permission universe the roles view
  and the docs lint both read. Reusing the existing stamp mechanism was preferred to a hand-written
  note in prose, which is the drift the generate-first rule exists to prevent.
- **Consequence (accepted, and the reason it is not a bug):** `operator` holds
  `component:create,update,rename,move` and **no `system:*` permission at all**; `deploy` holds
  `system:create,update,rename,move` beside the same component set. So an operator can no longer
  create a component INTO a system. That is the role line the seed already draws rather than damage
  from this change: an operator maintains components, a deploy tech builds out systems and their
  membership. The alternative, granting `operator` the `system:update` permission, was refused as a
  much larger grant than "may bind a membership while creating a component": it would also let an
  operator edit systems generally, which is exactly the line the two roles are drawn along. The
  recovery needs no grant at all, since creating the component and binding it are separately
  authorized acts: the operator creates it, and whoever holds the permission adds it after.
- **Consequence (the narrowing has to be met before the form is filled in):** a live narrowing
  discovered as a 403 after an operator has filled in a create form is the outcome
  [#699](https://github.com/hyperscaleav/omniglass/issues/699)'s rule exists to prevent, so the
  console does not offer the system picker to a principal that cannot use it, and the slot explains
  itself and names the permission rather than vanishing. The API's refusal names `system:update` too,
  which is what the CLI prints, because a refusal an operator cannot act on sends them to an
  administrator with no ask.

  **Amended (#707 review): that claim was FALSE as shipped, on the layer this decision itself calls
  out.** The console gate read the `system:update` **permission** and nothing else, while
  authorization is two layers and this decision moved BOTH. A principal holding the permission over
  no system at all was offered the picker, filled the form in, and was refused on submit, which is
  the exact outcome the paragraph promised to prevent. That principal is not exotic: a
  location-scoped `deploy` grant resolves `system:update` to the empty set, because
  `applicableKinds("system")` is `{"system"}` alone and the cross-tier expansion is unbuilt
  ([#10](https://github.com/hyperscaleav/omniglass/issues/10)), and devseed ships one as
  `tech-east`. The gate now also requires a system carrying the `update` **action**, the server's own
  per-row answer computed from the same per-action scope the gateway enforces, offers only those
  systems, and names whichever of the two layers is missing. The console filters data rather than
  resolving a scope in the browser, which it must not do.

  **Amended (#707 review): the API's refusal denied the row's existence.** The bind resolved in the
  `system:update` scope alone, so a system outside it came back as the non-disclosing
  `ErrSystemNotFound` and the route answered "system not found" (422) for a row the same caller could
  `GET`. The bind now takes `system:read` as well: update still decides whether it happens, read
  decides what the refusal may say. Out of the read scope is the same non-disclosing 422 as before,
  so nothing new is disclosed; inside it, the refusal is a **403 naming the scope**, which discloses
  nothing either, since that caller can read the row.
- **What this does NOT change:** the LOCATION reference on the same create still resolves in
  `location:read`, because a location is read and rendered into the label rather than written
  ([ADR-0100](#adr-0100-a-label-cascades-where-the-blast-radius-is-a-placement-and-waits-for-the-verb-where-it-is-the-estate),
  #700). The two references are deliberately not symmetric: what decides the action a placement
  reference resolves for is what the write DOES with it.

  **Amended (#713): the residual this entry left open is closed.** As shipped, the `:renderLabel`
  draft kept `system:read` on the reasoning that it renders a preview and writes nothing, and the
  entry accepted that a preview could therefore be served for a create the platform would refuse,
  with the console's own gate as the thing that kept the two in step. That is the caller's good
  behaviour standing in for the platform's, and it is also wrong about what the preview contains: the
  rendered string carries the system's TYPE label, so the lenient resolve hands back a fact about a
  row the caller may not bind, one tier past the disclosure `location:read` closes on the other
  reference. The draft now resolves its `system` through the create's own resolver, in
  `system:read` **and** `system:update`, and carries the create's conditional permission, so both
  halves of the gate rehearse and the refusals are the same sentinels in the same order. The
  LOCATION reference is unchanged on both routes, and remains `location:read` on both, for the reason
  above: it is read and rendered, never written.
- **Tracked under** [#707](https://github.com/hyperscaleav/omniglass/issues/707),
  [#713](https://github.com/hyperscaleav/omniglass/issues/713).
