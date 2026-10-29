-- migrate:up

-- Email as a notification channel (#1317, #1341). A target of kind email is a
-- fixed set of addresses (a shared mailbox), admin-owned like a webhook; a
-- group or person target may now deliver on email too, to each person's
-- directory email, resolved at send. The kind CHECK is replaced whole (drop,
-- then add) so it holds every kind to its own columns.
ALTER TABLE public.notification_target ADD COLUMN IF NOT EXISTS addresses text[] NOT NULL DEFAULT '{}';

ALTER TABLE public.notification_target DROP CONSTRAINT IF EXISTS notification_target_kind_check;
ALTER TABLE public.notification_target ADD CONSTRAINT notification_target_kind_check CHECK (
    (kind = 'webhook' AND url IS NOT NULL AND principal_group_id IS NULL AND principal_id IS NULL
        AND cardinality(channels) = 0 AND cardinality(addresses) = 0)
    OR (kind = 'email' AND url IS NULL AND principal_group_id IS NULL AND principal_id IS NULL
        AND secret_id IS NULL AND cardinality(channels) = 0 AND cardinality(addresses) > 0)
    OR (kind = 'principal_group' AND principal_group_id IS NOT NULL AND url IS NULL AND principal_id IS NULL
        AND secret_id IS NULL AND cardinality(addresses) = 0
        AND cardinality(channels) > 0 AND channels <@ ARRAY['inbox', 'email']::text[])
    OR (kind = 'principal' AND principal_id IS NOT NULL AND url IS NULL AND principal_group_id IS NULL
        AND secret_id IS NULL AND cardinality(addresses) = 0
        AND cardinality(channels) > 0 AND channels <@ ARRAY['inbox', 'email']::text[]));

-- migrate:down

ALTER TABLE public.notification_target DROP CONSTRAINT IF EXISTS notification_target_kind_check;
ALTER TABLE public.notification_target DROP COLUMN IF EXISTS addresses;
