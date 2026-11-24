---
title: "An admin's add makes an idp membership local, and a group's last mapping takes its idp members with it"
date: 2026-11-23
status: "Accepted"
summary: "Refines ADR-2361 point 4 where it is silent. One membership row per group and user, so an admin adding a member the provider brought turns that row local (it stops following the provider). Deleting a group's last mapping from an issuer removes that issuer's idp memberships of the group at once; deleting one of several waits for the next sync. The owner guard counts an owner grant at any scope. A refresh's id_token syncs only when it verifies, names the link's own subject, and carries the claim."
refs: [2363, 2361]
---

- **Date:** 2026-11-23 | **Status:** Accepted | **Pages:** [roles, groups, and grants](/admin/access/#identity-provider-groups), [identity and access](/architecture/identity-access/#credentials)
- **Decision:** (#2363, refining [ADR-2361](#adr-2361-single-sign-on-links-a-person-to-an-identity-provider-subject-never-by-email)
  points 4 and 9 where they are silent.)
  1. **One row per group and user.** `user_group_member` keeps its `(group_id, user_id)` key and gains
     `source` and `issuer`. An admin's add of a member the provider brought turns the row `local`,
     audited as an update: the admin's act is the stronger statement, and the row stops following the
     provider. An admin's remove of an `idp` row lasts until the person's next sync.
  2. **A group's last mapping takes its idp members with it.** Deleting a mapping removes, in the same
     audited transaction, the `idp` memberships of that group from that issuer when no mapping from the
     issuer is left, since no claim value could justify them. With others left, the memberships wait
     for each person's next sign-in or re-check, because which value put a person there is not stored.
  3. **"Carries an owner grant" means at any scope.** The guard refuses a mapping to a group with an
     `owner` grant of any scope, and an `owner` grant of any scope on a mapped group, both under a lock
     on the group row so the two sides cannot race. The sync also never joins such a group, a backstop
     for a row written around the API.
  4. **A refresh syncs only on a token it can trust for this link.** The id_token a refresh returns is
     verified (signature, issuer, audience, expiry; no nonce on a refresh); it syncs only when it names
     the link's own `iss|sub` and carries the groups claim. One that does not verify leaves the refresh
     accepted and the groups as they were, logged, rather than failing the refresh and spending the
     grace.
- **Context:** ADR-2361 fixed the model (claim values keyed by issuer, `local` and `idp` sources,
  fail closed on a missing claim at sign-in, the owner guard both ways) but not these edges, each of
  which the first build met.
- **Not chosen:** a second row per source (two rows for one membership, and every reader of the roster
  would need to collapse them); keeping `idp` rows after their last mapping goes (access with nothing
  behind it until the person happens to sign in); storing the claim value on each membership so any
  mapping delete could be exact (a wider row for a case the next sync settles); failing a refresh whose
  id_token does not verify (a provider's token quirk would suspend access at the grace).
