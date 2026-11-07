-- migrate:up
-- #1534: calculated metric series compact into blocks too (ADR-1534). Each
-- calculated sample carries the rule that wrote it (metric.source_rule,
-- source_rule_id, source_rule_version), so a block carries its one lineage:
-- null for an observed series, the rule's for a calculated one, whose fold
-- splits a span at every lineage change. The key gains the lineage, so a
-- span of a calculated series holds a block for each rule version that wrote
-- in it. A primary key cannot hold the nullable lineage, so the key is a
-- unique constraint with NULLS NOT DISTINCT, under which an observed series'
-- span still has exactly one block. The name is in the key because a rename
-- keeps the rule's version (RenameCalcRule).
ALTER TABLE public.metric_block
    ADD COLUMN IF NOT EXISTS source_rule text,
    ADD COLUMN IF NOT EXISTS source_rule_id uuid,
    ADD COLUMN IF NOT EXISTS source_rule_version bigint;

ALTER TABLE public.metric_block DROP CONSTRAINT IF EXISTS metric_block_pkey;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'metric_block_key') THEN
        ALTER TABLE public.metric_block ADD CONSTRAINT metric_block_key UNIQUE NULLS NOT DISTINCT
            (series_id, span_start, span, source_rule_id, source_rule_version, source_rule);
    END IF;
END $$;

-- migrate:down
-- A calculated series' blocks have no key without their lineage; the raw rows
-- they were folded from are authoritative, and the next fold of an observed
-- series is unchanged.
DELETE FROM public.metric_block
    WHERE source_rule IS NOT NULL OR source_rule_id IS NOT NULL OR source_rule_version IS NOT NULL;
ALTER TABLE public.metric_block DROP CONSTRAINT IF EXISTS metric_block_key;
ALTER TABLE public.metric_block
    DROP COLUMN IF EXISTS source_rule_version,
    DROP COLUMN IF EXISTS source_rule_id,
    DROP COLUMN IF EXISTS source_rule;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'metric_block_pkey') THEN
        ALTER TABLE public.metric_block ADD CONSTRAINT metric_block_pkey PRIMARY KEY (series_id, span_start, span);
    END IF;
END $$;
