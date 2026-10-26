---
title: "Pre-tag hygiene, the API and data half (#555, #260, #558, #570, #808)"
date: 2026-10-26
refs: [555, 260, 558, 570, 808, 825]
---

Pre-tag hygiene for 0.1.0 (#825), the contract half: each is cheapest before a public release
exists.

**A registry renames (#555,
[ADR-0555](/architecture/decisions/#adr-0555-a-registry-row-renames-through-its-own-rename-custom-method)).**
ADR-0062 gave every registry a uuid primary key so its name could change, and nothing but raw SQL
could change it. `POST /<registry>/{id}:rename` now moves the name of a vendor, driver, product,
standard or location type, by its current name or its uuid, gated by its own `<registry>:rename`
(admin) and served by one gateway primitive, `RenameRegistry`. Every foreign key holds the uuid, so
every referencing row reads the new name; a location type's by-value copies in
`allowed_parent_types` and in forks move in the same transaction, and a driver's attached tasks
re-derive under the new name. An official row keeps its shipped name, and so does a shipped example
standard (409), which the boot seed plants by name whenever it is absent (`standard.seeded` marks
them; #827 forks templates instead). The rename is audited. The CLI gains `omniglass <registry> rename`. The remaining
registries are #1188.

**A product's references can be cleared (#260).** The product patch wrote its nullable columns with
`coalesce`, and the API collapsed an explicit empty string to nil first, so a vendor, driver, parent
or icon could be set and never unset: the console's **None** reverted on refetch. They now follow the
house three-state string, the same `CASE` the inherited registry facts use (omitted is unchanged,
`""` clears to NULL, a value sets), and the blade sends an emptied picker as the clear.

**The encryption key is named for what it is (#558,
[ADR-0558](/architecture/decisions/#adr-0558-the-key-that-seals-every-secret-is-the-encryption-key-and-its-old-names-are-read-for-one-release)).**
`OMNIGLASS_ENCRYPTION_KEY` and `OMNIGLASS_ENCRYPTION_KEY_FILE` replace the `SECRET_KEY` pair, which
read as a secret's own key. The old names are still read while neither new one is set, warning on
every boot with the replacement, so an upgrade never locks an operator out of their secrets.

**The node table stops carrying its own name rule (#570).** A new migration drops
`node_name_subject_safe_check`, a looser rule than the entity name rule the gateway applies first, so
it never fired and only misdescribed the identifier; its two unreachable mirrors in Go go with it,
and a gateway test refuses every character the CHECK used to catch.

**A console miss that names a file at the root is a 404 (#808).** #778's build-directory rule could
not see the root, where `public/` lands a contributor's file, so a typo of one answered the shell with
200. A miss whose last segment carries an extension is now a 404 too, resting on the premise that no
console route carries a dot, which a test reads off the route manifest; a build directory is a 404
rather than a listing of its hashed assets.
