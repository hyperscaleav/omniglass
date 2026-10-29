## scope-blind-directory-check (fixed; found by the #1352 review)

- **Cue:** `a.allows(ctx, "<directory>", "read", "admin")` (principal, principal_group, role) standing
  in for "may read this directory" outside the directory's own routes: a field shown or withheld on
  it, or a binding admitted on it.
- **Failure:** the flattened permission set ignores where a grant is scoped, so an `admin` grant
  scoped to one component carries `principal:read:admin` while the directory's own list refuses it
  (`ListPrincipals` takes an all-scope grant). The stand-in check then shows that caller the names
  the directory hides: a notification's recipient, the person a target names.
- **Fix:** take both halves, the permission and an all-scope grant of it, through
  `canReadDirectory` (internal/api/principals.go), the predicate `canListPrincipals` uses.
