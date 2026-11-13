-- migrate:up
-- #1627: a server's platform component, the component its request and
-- database figures land on in the platform system (#1094). A server has no
-- row of its own elsewhere, so this table is its identity: the name it boots
-- under (OMNIGLASS_SERVER_NAME, else its hostname) and the component it
-- reports on. Unique, since a component is one server's at most. No action on
-- delete: the component cannot go while a server names it.
CREATE TABLE IF NOT EXISTS public.platform_server (
    name text PRIMARY KEY,
    platform_component_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'platform_server_platform_component_id_fkey') THEN
        ALTER TABLE public.platform_server ADD CONSTRAINT platform_server_platform_component_id_fkey
            FOREIGN KEY (platform_component_id) REFERENCES public.component (id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'platform_server_platform_component_id_key') THEN
        ALTER TABLE public.platform_server ADD CONSTRAINT platform_server_platform_component_id_key UNIQUE (platform_component_id);
    END IF;
END $$;

-- migrate:down
DROP TABLE IF EXISTS public.platform_server;
