---
title: "Creating a node in the console no longer enrolls it (#287)"
date: 2026-11-12
refs: [287, 280, 1203]
---

The console's create form used to chain `POST /nodes` into `POST /nodes/{name}:enroll`, so a
principal with `node:create` alone could not create a node and one with both got a token minted
whether or not the device was ready to claim it. The two are now separate acts, as they are in the
API: the form posts the create, closes, opens the new node's blade and toasts "Node created. Enroll
it when the device is ready.", and the create button is gated on `node:create` alone. The blade
carries Enroll as its primary action while the node has never been enrolled, and Re-enroll stays in
the kebab afterwards; both stay gated by `node:enroll`. Tests assert a create issues exactly one
request and none to `:enroll`, that a `node:create`-only principal can create and sees no Enroll,
and that the unenrolled blade's primary Enroll reveals the token once.
