-- migrate:up

-- The single-owner invariant (ADR-0006): at least one `owner @ all` grant must
-- exist at all times, so the platform can never be locked out of administration.
-- Enforced as a DEFERRABLE INITIALLY DEFERRED constraint trigger, so the check
-- runs at COMMIT rather than per statement: revoking the old owner and granting a
-- new one in one transaction (the swap-owner pattern) is allowed, while revoking
-- the last owner is refused. The custom SQLSTATE OG001 lets the gateway map it to
-- a clean 409 rather than a generic 500.
create or replace function assert_owner_grant_exists() returns trigger
    language plpgsql as $$
begin
    if not exists (
        select 1 from principal_grant where role_id = 'owner' and scope_kind = 'all'
    ) then
        raise exception 'at least one owner grant must remain'
            using errcode = 'OG001';
    end if;
    return null;
end;
$$;

drop trigger if exists principal_grant_owner_guard on principal_grant;
create constraint trigger principal_grant_owner_guard
    after delete or update on principal_grant
    deferrable initially deferred
    for each row
    execute function assert_owner_grant_exists();

-- migrate:down
drop trigger if exists principal_grant_owner_guard on principal_grant;
drop function if exists assert_owner_grant_exists();
