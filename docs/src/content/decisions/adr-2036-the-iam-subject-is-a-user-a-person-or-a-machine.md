---
title: "The IAM subject is a user, a person or a machine"
date: 2026-11-16
status: "Accepted"
summary: "Principal retires for user on every surface, the table included (users, since user is reserved in Postgres). A user's kind is person or machine; an outpost is a machine user its outpost row points at. A set of users is a user group, so IAM no longer owns the bare word group"
refs: [2036, 2028, 2075, 2076]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [glossary](/architecture/glossary/),
  [identity and access](/architecture/identity-access/), [storage](/architecture/storage/)
- **Decision:** (#2036.) The subject a grant is given to is a **user**, on every surface: the
  schema, the Go names, the API (`/users`, `/user-groups`), the permissions (`user:*`,
  `user_grant:*`, `user_group:*`), the CLI, the console and the docs. **Principal** retires and
  joins the denylist. The table is **`users`**, the one plural table, because `user` is a reserved
  word in Postgres; every sibling stays singular (`user_id`, `user_grant`, `user_group`,
  `user_label`). A user's **kind** is **person** or **machine**, the question an operator asks
  (is a human behind it): a person signs in with a password, carries an avatar and receives
  notifications; a machine user holds tokens. `human` becomes `person`, `service` becomes
  `machine`, and **service account** becomes **machine user**. An **outpost** is a machine user
  its outpost row points at, so `kind = 'node'` retires and code that needs to know reads the
  outpost row. The `agent` kind reserved by
  [ADR-0001](/architecture/decisions/#adr-0001-ai-acts-as-a-user-the-agent-principal-is-deferred)
  is dropped: until a first-class agent is built (and decides its own kind), an AI acts as a person
  or a machine user. A set of users granted together is a **user group**; IAM gives up the bare
  word **group**, so the design-stage entity group (#10) qualifies itself when it is built, and
  system nesting (#879) keeps parent system and subsystem. **Account** stays plain English for a
  user's sign-in state (a locked account, your own account), never a second noun for the row;
  `username` keeps its name. The Postgres role in a connection string is a database role, not a
  user. The renames are #2075 (schema, API, CLI) then #2076 (console, docs).
- **Context:** the API, schema and permissions said principal (185 OpenAPI hits, about 1,940 in
  non-test Go, 796 in docs) while the console said Users at `/users` and Groups at `/groups`, and
  its own hint read "Users and service accounts": the route drifted from the API and the label
  already counted machines as users. Principal is exact IAM vocabulary, but Omniglass's operators
  are AV and IT staff, not IAM specialists, and the product already speaks of a person and people
  where it addresses humans (191 docs hits, the `notification_people` migration). User stretching
  to machines has precedent in Unix (`www-data` is a user) and AWS IAM; Kubernetes splitting users
  from service accounts was the counter-example, rejected because the console already merges them.
  Considered and rejected: keeping principal and renaming the console page Principals (exact, but
  the word an operator does not say); user on surfaces only with principal in schema and Go (two
  words for one row, what [ADR-2035](/architecture/decisions/#adr-2035-the-catalog-is-what-the-fleet-is-typed-by-a-registry-is-compiled-in)
  rejected); `account` as the table (a second noun beside user) and a quoted `"user"` table
  (quoting forever). For kinds, person, service and outpost 1:1 with the detail tables was
  considered; it kept `service`, which collides with a product's `service` kind, and kept a kind
  that the outpost row already answers. The pre-deployment stance makes this the cheapest the
  rename will ever be: one migration, no compatibility layer.
