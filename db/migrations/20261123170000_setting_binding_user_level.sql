-- migrate:up
-- #2450: the settings cascade's user level (the user rung of #270). A row at
-- scope 'user' is one person's binding for a profile-domain namespace and
-- carries their user_id; a platform row carries none. The scope CHECK widens
-- to the two persisted levels and ties each to its user_id shape, so a user
-- row without its person, or a platform row with one, cannot be written.
-- The person's rows go with the person.

ALTER TABLE public.setting_binding DROP CONSTRAINT IF EXISTS setting_binding_scope_check;
ALTER TABLE public.setting_binding ADD CONSTRAINT setting_binding_scope_check CHECK (
  (scope = 'platform' AND user_id IS NULL) OR (scope = 'user' AND user_id IS NOT NULL)
);

ALTER TABLE public.setting_binding DROP CONSTRAINT IF EXISTS setting_binding_user_fk;
ALTER TABLE public.setting_binding ADD CONSTRAINT setting_binding_user_fk
  FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

-- migrate:down
ALTER TABLE public.setting_binding DROP CONSTRAINT IF EXISTS setting_binding_user_fk;
DELETE FROM public.setting_binding WHERE scope = 'user';
ALTER TABLE public.setting_binding DROP CONSTRAINT IF EXISTS setting_binding_scope_check;
ALTER TABLE public.setting_binding ADD CONSTRAINT setting_binding_scope_check CHECK ((scope = 'platform'::text));
