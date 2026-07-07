-- migrate:up

-- Generalize the exclude_root boolean into a scope operator: how a grant's
-- scope_id matches the tree. 'subtree' is root + descendants (the old
-- exclude_root=false); 'subtree_excl_root' is descendants only for update/delete,
-- root kept for read/create (the old exclude_root=true); 'self' is the root row
-- only, no descendant walk (net-new: a grant on exactly one node). The default
-- preserves every existing grant's inclusive behavior.
alter table principal_grant add column if not exists scope_op text not null default 'subtree'
    check (scope_op in ('subtree', 'subtree_excl_root', 'self'));

-- Carry the just-shipped boolean forward: an excluded root becomes subtree_excl_root.
update principal_grant set scope_op = 'subtree_excl_root' where exclude_root;

-- The dedup index must include scope_op: two grants that differ only in their
-- operator (subtree vs self on the same root, say) are distinct grants, not a
-- duplicate. The prior index omitted it and rejected the second as a collision.
drop index if exists principal_grant_unique;
create unique index if not exists principal_grant_unique
    on principal_grant (principal_id, role_id, scope_kind, coalesce(scope_id, ''), scope_op);

alter table principal_grant drop column if exists exclude_root;

-- migrate:down
-- Best-effort reversal (dev only): if the operator capability was exercised (two
-- grants on one root differing only by scope_op, or any self grant), recreating the
-- op-less unique index below can fail on a now-duplicate key, and self collapses to
-- exclude_root=false (a widening). Rolling back removes the feature that gave those
-- grants meaning, so this is acceptable for the ephemeral dev database.
alter table principal_grant add column if not exists exclude_root boolean not null default false;
update principal_grant set exclude_root = true where scope_op = 'subtree_excl_root';
drop index if exists principal_grant_unique;
create unique index if not exists principal_grant_unique
    on principal_grant (principal_id, role_id, scope_kind, coalesce(scope_id, ''));
alter table principal_grant drop column if exists scope_op;
