-- migrate:up
-- #1625: a node's platform component, the component its self-report lands on
-- in the platform system (#1094). The node row names its component, so
-- nothing has to match on names; unique, since a component is one node's at
-- most. No action on delete: the component cannot go while the node names it,
-- and the gateway removes the node and then its component in one transaction.
ALTER TABLE public.node
    ADD COLUMN IF NOT EXISTS platform_component_id uuid;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'node_platform_component_id_fkey') THEN
        ALTER TABLE public.node ADD CONSTRAINT node_platform_component_id_fkey
            FOREIGN KEY (platform_component_id) REFERENCES public.component (id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'node_platform_component_id_key') THEN
        ALTER TABLE public.node ADD CONSTRAINT node_platform_component_id_key UNIQUE (platform_component_id);
    END IF;
END $$;

-- migrate:down
ALTER TABLE public.node DROP CONSTRAINT IF EXISTS node_platform_component_id_key;
ALTER TABLE public.node DROP CONSTRAINT IF EXISTS node_platform_component_id_fkey;
ALTER TABLE public.node DROP COLUMN IF EXISTS platform_component_id;
