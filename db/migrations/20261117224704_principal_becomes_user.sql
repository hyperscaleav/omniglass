-- migrate:up
-- #2075 (ADR-2036): the IAM subject is a user, its kinds are person and
-- machine, and a set of users is a user group. The rename carries the four
-- principal tables (principal itself becomes users, the one plural table,
-- since user is reserved in Postgres), the human and service detail tables
-- (person and machine), every principal column, and every constraint, index,
-- sequence and trigger named for them (a constraint's backing index renames
-- with the constraint).
--
-- Three value sets follow the names. A user's kind is person or machine: a
-- human is a person, and a service account or an outpost is a machine (an
-- outpost's machine user is the one its outpost row points at; agent was
-- never written). A notification target that reaches a user or a user group
-- says so. A role's permissions name the user, user_grant and user_group
-- resources, so an operator's own role keeps working. The audit log's
-- resource strings are history and stay as written.
--
-- The whole migration is one transaction, so it is guarded once: a database
-- that already has the users table is left as it is.

DO $$
DECLARE
  r record;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.tables
                 WHERE table_schema = 'public' AND table_name = 'principal') THEN
    RETURN;
  END IF;

  ALTER TABLE public.principal RENAME TO users;
  ALTER TABLE public.principal_group RENAME TO user_group;
  ALTER TABLE public.principal_grant RENAME TO user_grant;
  ALTER TABLE public.principal_group_member RENAME TO user_group_member;
  ALTER TABLE public.human RENAME TO person;
  ALTER TABLE public.service RENAME TO machine;
  ALTER SEQUENCE public.principal_seq_seq RENAME TO users_seq_seq;

  FOR r IN
    SELECT table_name, column_name
      FROM information_schema.columns
     WHERE table_schema = 'public' AND column_name ~ 'principal'
  LOOP
    EXECUTE format('ALTER TABLE public.%I RENAME COLUMN %I TO %I', r.table_name, r.column_name,
      replace(r.column_name, 'principal', 'user'));
  END LOOP;

  -- Constraints, then the indexes left. A name spelled for the group or grant
  -- table takes user_group or user_grant, for the principal table users_, for
  -- a column user; the detail tables' own prefixes follow their tables.
  FOR r IN
    SELECT con.conname, cls.relname
      FROM pg_constraint con
      JOIN pg_class cls ON cls.oid = con.conrelid
      JOIN pg_namespace n ON n.oid = cls.relnamespace
     WHERE n.nspname = 'public'
       AND (con.conname ~ 'principal'
            OR (cls.relname = 'person' AND con.conname ~ '^human_')
            OR (cls.relname = 'machine' AND con.conname ~ '^service_'))
  LOOP
    EXECUTE format('ALTER TABLE public.%I RENAME CONSTRAINT %I TO %I', r.relname, r.conname,
      regexp_replace(regexp_replace(regexp_replace(regexp_replace(regexp_replace(r.conname,
        'principal_(group|grant)', 'user_\1', 'g'), '^principal_', 'users_'),
        'principal', 'user', 'g'), '^human_', 'person_'), '^service_', 'machine_'));
  END LOOP;
  FOR r IN
    SELECT c.relname
      FROM pg_class c
      JOIN pg_namespace n ON n.oid = c.relnamespace
      JOIN pg_index i ON i.indexrelid = c.oid
      JOIN pg_class t ON t.oid = i.indrelid
     WHERE n.nspname = 'public' AND c.relkind = 'i'
       AND (c.relname ~ 'principal'
            OR (t.relname = 'person' AND c.relname ~ '^human_')
            OR (t.relname = 'machine' AND c.relname ~ '^service_'))
  LOOP
    EXECUTE format('ALTER INDEX public.%I RENAME TO %I', r.relname,
      regexp_replace(regexp_replace(regexp_replace(regexp_replace(regexp_replace(r.relname,
        'principal_(group|grant)', 'user_\1', 'g'), '^principal_', 'users_'),
        'principal', 'user', 'g'), '^human_', 'person_'), '^service_', 'machine_'));
  END LOOP;

  ALTER TRIGGER principal_grant_owner_guard ON public.user_grant RENAME TO user_grant_owner_guard;

  ALTER TABLE public.users DROP CONSTRAINT users_kind_check;
  UPDATE public.users SET kind = 'person' WHERE kind = 'human';
  UPDATE public.users SET kind = 'machine' WHERE kind IN ('service', 'outpost');
  ALTER TABLE public.users ADD CONSTRAINT users_kind_check CHECK (kind IN ('person', 'machine'));

  ALTER TABLE public.notification_target DROP CONSTRAINT notification_target_kind_check;
  UPDATE public.notification_target SET kind = 'user' WHERE kind = 'principal';
  UPDATE public.notification_target SET kind = 'user_group' WHERE kind = 'principal_group';
  ALTER TABLE public.notification_target ADD CONSTRAINT notification_target_kind_check CHECK (
    (kind = 'webhook' AND url IS NOT NULL AND user_group_id IS NULL AND user_id IS NULL
      AND cardinality(channels) = 0 AND cardinality(addresses) = 0)
    OR (kind = 'email' AND url IS NULL AND user_group_id IS NULL AND user_id IS NULL
      AND secret_id IS NULL AND cardinality(channels) = 0 AND cardinality(addresses) > 0)
    OR (kind = 'user_group' AND user_group_id IS NOT NULL AND url IS NULL AND user_id IS NULL
      AND secret_id IS NULL AND cardinality(addresses) = 0 AND cardinality(channels) > 0
      AND channels <@ ARRAY['inbox', 'email'])
    OR (kind = 'user' AND user_id IS NOT NULL AND url IS NULL AND user_group_id IS NULL
      AND secret_id IS NULL AND cardinality(addresses) = 0 AND cardinality(channels) > 0
      AND channels <@ ARRAY['inbox', 'email']));

  UPDATE public.role SET permissions = ARRAY(
    SELECT regexp_replace(p, '^principal(_grant|_group)?:', 'user\1:')
      FROM unnest(permissions) WITH ORDINALITY AS u(p, i) ORDER BY i)
   WHERE EXISTS (SELECT 1 FROM unnest(permissions) p WHERE p ~ '^principal');
END $$;

CREATE OR REPLACE FUNCTION public.assert_owner_grant_exists() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
begin
    if not exists (
        select 1 from user_grant
        where role_id = (select id from role where name = 'owner') and scope_kind = 'all'
    ) then
        raise exception 'at least one owner grant must remain'
            using errcode = 'OG001';
    end if;
    return null;
end;
$$;

-- migrate:down
DO $$
DECLARE
  r record;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.tables
                 WHERE table_schema = 'public' AND table_name = 'users') THEN
    RETURN;
  END IF;

  UPDATE public.role SET permissions = ARRAY(
    SELECT regexp_replace(p, '^user(_grant|_group)?:', 'principal\1:')
      FROM unnest(permissions) WITH ORDINALITY AS u(p, i) ORDER BY i)
   WHERE EXISTS (SELECT 1 FROM unnest(permissions) p WHERE p ~ '^user(_grant|_group)?:');

  ALTER TABLE public.notification_target DROP CONSTRAINT notification_target_kind_check;
  UPDATE public.notification_target SET kind = 'principal' WHERE kind = 'user';
  UPDATE public.notification_target SET kind = 'principal_group' WHERE kind = 'user_group';
  ALTER TABLE public.notification_target ADD CONSTRAINT notification_target_kind_check CHECK (
    (kind = 'webhook' AND url IS NOT NULL AND user_group_id IS NULL AND user_id IS NULL
      AND cardinality(channels) = 0 AND cardinality(addresses) = 0)
    OR (kind = 'email' AND url IS NULL AND user_group_id IS NULL AND user_id IS NULL
      AND secret_id IS NULL AND cardinality(channels) = 0 AND cardinality(addresses) > 0)
    OR (kind = 'principal_group' AND user_group_id IS NOT NULL AND url IS NULL AND user_id IS NULL
      AND secret_id IS NULL AND cardinality(addresses) = 0 AND cardinality(channels) > 0
      AND channels <@ ARRAY['inbox', 'email'])
    OR (kind = 'principal' AND user_id IS NOT NULL AND url IS NULL AND user_group_id IS NULL
      AND secret_id IS NULL AND cardinality(addresses) = 0 AND cardinality(channels) > 0
      AND channels <@ ARRAY['inbox', 'email']));

  ALTER TABLE public.users DROP CONSTRAINT users_kind_check;
  UPDATE public.users SET kind = 'human' WHERE kind = 'person';
  UPDATE public.users u SET kind = 'outpost'
   WHERE kind = 'machine' AND EXISTS (SELECT 1 FROM public.outpost o WHERE o.user_id = u.id);
  UPDATE public.users SET kind = 'service' WHERE kind = 'machine';
  ALTER TABLE public.users ADD CONSTRAINT users_kind_check
    CHECK (kind IN ('human', 'service', 'outpost', 'agent'));

  ALTER TRIGGER user_grant_owner_guard ON public.user_grant RENAME TO principal_grant_owner_guard;

  -- Only the names this migration wrote: user_agent and username columns,
  -- and anything named for them, were never principal and are not touched.
  FOR r IN
    SELECT con.conname, cls.relname
      FROM pg_constraint con
      JOIN pg_class cls ON cls.oid = con.conrelid
      JOIN pg_namespace n ON n.oid = cls.relnamespace
     WHERE n.nspname = 'public'
       AND (con.conname ~ '(^users_|user_group|user_grant|_user_id|_user_idx)'
            OR (cls.relname = 'person' AND con.conname ~ '^person_')
            OR (cls.relname = 'machine' AND con.conname ~ '^machine_'))
  LOOP
    EXECUTE format('ALTER TABLE public.%I RENAME CONSTRAINT %I TO %I', r.relname, r.conname,
      regexp_replace(regexp_replace(regexp_replace(regexp_replace(regexp_replace(regexp_replace(r.conname,
        '^person_', 'human_'), '^machine_', 'service_'), 'user_(group|grant)', 'principal_\1', 'g'),
        '^users_', 'principal_'), '_user_id', '_principal_id', 'g'), '_user_idx', '_principal_idx', 'g'));
  END LOOP;
  FOR r IN
    SELECT c.relname
      FROM pg_class c
      JOIN pg_namespace n ON n.oid = c.relnamespace
      JOIN pg_index i ON i.indexrelid = c.oid
      JOIN pg_class t ON t.oid = i.indrelid
     WHERE n.nspname = 'public' AND c.relkind = 'i'
       AND (c.relname ~ '(^users_|user_group|user_grant|_user_id|_user_idx)'
            OR (t.relname = 'person' AND c.relname ~ '^person_')
            OR (t.relname = 'machine' AND c.relname ~ '^machine_'))
  LOOP
    EXECUTE format('ALTER INDEX public.%I RENAME TO %I', r.relname,
      regexp_replace(regexp_replace(regexp_replace(regexp_replace(regexp_replace(regexp_replace(r.relname,
        '^person_', 'human_'), '^machine_', 'service_'), 'user_(group|grant)', 'principal_\1', 'g'),
        '^users_', 'principal_'), '_user_id', '_principal_id', 'g'), '_user_idx', '_principal_idx', 'g'));
  END LOOP;

  FOR r IN
    SELECT table_name, column_name
      FROM information_schema.columns
     WHERE table_schema = 'public' AND column_name ~ '(^|_)user(_group)?_id$'
  LOOP
    EXECUTE format('ALTER TABLE public.%I RENAME COLUMN %I TO %I', r.table_name, r.column_name,
      replace(r.column_name, 'user', 'principal'));
  END LOOP;

  ALTER SEQUENCE public.users_seq_seq RENAME TO principal_seq_seq;
  ALTER TABLE public.machine RENAME TO service;
  ALTER TABLE public.person RENAME TO human;
  ALTER TABLE public.user_group_member RENAME TO principal_group_member;
  ALTER TABLE public.user_grant RENAME TO principal_grant;
  ALTER TABLE public.user_group RENAME TO principal_group;
  ALTER TABLE public.users RENAME TO principal;
END $$;

CREATE OR REPLACE FUNCTION public.assert_owner_grant_exists() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
begin
    if not exists (
        select 1 from principal_grant
        where role_id = (select id from role where name = 'owner') and scope_kind = 'all'
    ) then
        raise exception 'at least one owner grant must remain'
            using errcode = 'OG001';
    end if;
    return null;
end;
$$;
