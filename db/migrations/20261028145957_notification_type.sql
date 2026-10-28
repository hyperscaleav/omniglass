-- migrate:up

-- The notification_type registry (#618, #1319): the outbound peer of
-- command_type, fleet-wide reference data saying WHAT a message says. Two
-- operator-authored templates, rendered through the shared template sandbox
-- over the alarm fact map: subject (one line) and text (the body). Official
-- rows ship in the boot seed (authoritative upsert) and are read-only; an
-- operator adds their own. A row says nothing about WHEN it is sent or WHERE:
-- that is the action rule's and the target's.
CREATE TABLE IF NOT EXISTS public.notification_type (
    id uuid DEFAULT uuidv7() PRIMARY KEY,
    name text NOT NULL CONSTRAINT notification_type_name_key UNIQUE,
    label text,
    description text NOT NULL DEFAULT '',
    subject text NOT NULL DEFAULT '',
    text text NOT NULL DEFAULT '',
    official boolean NOT NULL DEFAULT false,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now()
);

-- migrate:down

DROP TABLE IF EXISTS public.notification_type;
