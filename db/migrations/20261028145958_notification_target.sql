-- migrate:up

-- The notification_target (#618, #1320): WHERE a message goes. An operator
-- row naming a channel kind (a code-declared enum, webhook the first), its
-- destination URL, an optional credential bound by uuid (a platform secret of
-- the hmac-key or bearer-token type, which the send signs or authenticates
-- with), and an enabled flag. Platform-owned: no owner arc, administered by
-- admins. The URL is itself often a credential (a chat incoming-webhook URL),
-- so the API masks it on read and reveals it audited. A secret a target binds
-- cannot be deleted from under it (RESTRICT): the delete is refused, naming
-- the target, rather than leaving a send that silently stops signing.
CREATE TABLE IF NOT EXISTS public.notification_target (
    id uuid DEFAULT uuidv7() PRIMARY KEY,
    name text NOT NULL CONSTRAINT notification_target_name_key UNIQUE,
    label text,
    kind text NOT NULL,
    url text NOT NULL,
    secret_id uuid REFERENCES public.secret(id) ON DELETE RESTRICT,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS notification_target_secret_idx ON public.notification_target (secret_id) WHERE secret_id IS NOT NULL;

-- migrate:down

DROP TABLE IF EXISTS public.notification_target;
