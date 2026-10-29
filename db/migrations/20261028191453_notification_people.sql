-- migrate:up

-- People as notification recipients (#1341, #1352). A notification row may be
-- addressed to one person on one channel: recipient_principal_id (set null
-- when the principal is purged, the history kept by recipient_name, as the
-- rule, type and target already are) and channel, the way it travels
-- (webhook, email, inbox). A row about a person is one person on one channel,
-- so each person has their own delivery state, retry and Idempotency-Key. An
-- inbox row is delivered by its own commit and carries read_at, set when its
-- recipient reads it.
ALTER TABLE public.notification ADD COLUMN IF NOT EXISTS recipient_principal_id uuid;
ALTER TABLE public.notification ADD COLUMN IF NOT EXISTS recipient_name text;
ALTER TABLE public.notification ADD COLUMN IF NOT EXISTS channel text NOT NULL DEFAULT 'webhook';
ALTER TABLE public.notification ADD COLUMN IF NOT EXISTS read_at timestamp with time zone;

DO $$
BEGIN
    ALTER TABLE public.notification ADD CONSTRAINT notification_recipient_principal_id_fkey
        FOREIGN KEY (recipient_principal_id) REFERENCES public.principal(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- A webhook row has no person; an inbox row always has one; an email row has
-- one when it is a person's, none when it goes to a fixed address.
DO $$
BEGIN
    ALTER TABLE public.notification ADD CONSTRAINT notification_channel_check CHECK (
        channel IN ('webhook', 'email', 'inbox')
        AND (channel <> 'webhook' OR recipient_name IS NULL)
        AND (channel <> 'inbox' OR recipient_name IS NOT NULL));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- The inbox read: a person's rows, newest first.
CREATE INDEX IF NOT EXISTS notification_recipient_idx ON public.notification (recipient_principal_id, created_at DESC)
    WHERE recipient_principal_id IS NOT NULL;

-- A target may name people: a principal group, expanded to its members at
-- every alarm transition, or one principal, each bound by uuid and carrying
-- the channels it delivers on (inbox, email). A webhook keeps its URL; a
-- person target has none. A bound group or principal cannot be deleted from
-- under a target (RESTRICT), as a bound secret cannot.
ALTER TABLE public.notification_target ALTER COLUMN url DROP NOT NULL;
ALTER TABLE public.notification_target ADD COLUMN IF NOT EXISTS principal_group_id uuid;
ALTER TABLE public.notification_target ADD COLUMN IF NOT EXISTS principal_id uuid;
ALTER TABLE public.notification_target ADD COLUMN IF NOT EXISTS channels text[] NOT NULL DEFAULT '{}';

DO $$
BEGIN
    ALTER TABLE public.notification_target ADD CONSTRAINT notification_target_principal_group_id_fkey
        FOREIGN KEY (principal_group_id) REFERENCES public.principal_group(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE public.notification_target ADD CONSTRAINT notification_target_principal_id_fkey
        FOREIGN KEY (principal_id) REFERENCES public.principal(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE public.notification_target ADD CONSTRAINT notification_target_kind_check CHECK (
        (kind = 'webhook' AND url IS NOT NULL AND principal_group_id IS NULL AND principal_id IS NULL
            AND cardinality(channels) = 0)
        OR (kind = 'principal_group' AND principal_group_id IS NOT NULL AND url IS NULL AND principal_id IS NULL
            AND secret_id IS NULL AND cardinality(channels) > 0 AND channels <@ ARRAY['inbox', 'email']::text[])
        OR (kind = 'principal' AND principal_id IS NOT NULL AND url IS NULL AND principal_group_id IS NULL
            AND secret_id IS NULL AND cardinality(channels) > 0 AND channels <@ ARRAY['inbox', 'email']::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE INDEX IF NOT EXISTS notification_target_principal_group_idx ON public.notification_target (principal_group_id)
    WHERE principal_group_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS notification_target_principal_idx ON public.notification_target (principal_id)
    WHERE principal_id IS NOT NULL;

-- migrate:down

ALTER TABLE public.notification_target DROP CONSTRAINT IF EXISTS notification_target_kind_check;
DROP INDEX IF EXISTS public.notification_target_principal_idx;
DROP INDEX IF EXISTS public.notification_target_principal_group_idx;
ALTER TABLE public.notification_target DROP COLUMN IF EXISTS channels;
ALTER TABLE public.notification_target DROP COLUMN IF EXISTS principal_id;
ALTER TABLE public.notification_target DROP COLUMN IF EXISTS principal_group_id;
DROP INDEX IF EXISTS public.notification_recipient_idx;
ALTER TABLE public.notification DROP CONSTRAINT IF EXISTS notification_channel_check;
ALTER TABLE public.notification DROP COLUMN IF EXISTS read_at;
ALTER TABLE public.notification DROP COLUMN IF EXISTS channel;
ALTER TABLE public.notification DROP COLUMN IF EXISTS recipient_name;
ALTER TABLE public.notification DROP COLUMN IF EXISTS recipient_principal_id;
