-- migrate:up
-- #1275 (and #420's partitioning half), first of three: the narrow metric
-- lane. A metric row was about 126 bytes of heap and 90 of index, most of it
-- the series it belongs to (owner arc, type, instance, provenance, source)
-- repeated on every observation. The series moves to a dimension of its own,
-- metric_series, keyed by an integer, and a sample row keeps only
-- (id, series_id, ts, value), with a calculated row's lineage in nullable
-- columns that cost nothing when empty.
--
-- The provenance split: observed and calculated rows go in metric, range
-- partitioned by month from birth so retention drops a month whole; a
-- command's intended rows go in metric_declared, a plain table no retention
-- pass touches. ADR-1275.
--
-- This migration is the create half of one create-copy-swap: the wide table
-- steps aside as metric_wide (with its indexes and sequence, so the new
-- table's names are free), the narrow tables are created empty, and one
-- partition is created for every month the wide table holds a row in, plus
-- this month and the two after it. The next migration copies the rows and
-- verifies the count; the one after drops metric_wide.

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM pg_class WHERE oid = to_regclass('public.metric') AND relkind = 'r')
     AND to_regclass('public.metric_wide') IS NULL THEN
    ALTER TABLE public.metric RENAME TO metric_wide;
    ALTER INDEX IF EXISTS public.metric_pkey RENAME TO metric_wide_pkey;
    ALTER INDEX IF EXISTS public.metric_owner_idx RENAME TO metric_wide_owner_idx;
    ALTER INDEX IF EXISTS public.metric_arc_owner_idx RENAME TO metric_wide_arc_owner_idx;
    ALTER INDEX IF EXISTS public.metric_ts_brin RENAME TO metric_wide_ts_brin;
    ALTER SEQUENCE IF EXISTS public.metric_id_seq RENAME TO metric_wide_id_seq;
  END IF;
END $$;

-- One row per series: what a metric sample is a sample of. owner_id is the
-- arc's one filled column, so the series key and its readers need not know
-- which. collector_node_id is the node whose task collected the series (null
-- for the push lane, a calc rule, a command, and every row from before it was
-- recorded); a task that moves to another node starts a new series of the
-- same logical key (type, owner, instance, provenance), which readers merge.
-- It is a record, not a reference: a deleted node leaves its series' history.
CREATE TABLE IF NOT EXISTS public.metric_series (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    metric_type_id uuid NOT NULL REFERENCES public.metric_type(id) ON DELETE CASCADE,
    owner_kind text NOT NULL,
    component_id uuid REFERENCES public.component(id) ON DELETE CASCADE,
    system_id uuid REFERENCES public.system(id) ON DELETE CASCADE,
    location_id uuid REFERENCES public.location(id) ON DELETE CASCADE,
    node_id uuid REFERENCES public.node(principal_id) ON DELETE CASCADE,
    owner_id uuid GENERATED ALWAYS AS (COALESCE(component_id, system_id, location_id, node_id)) STORED,
    instance text DEFAULT ''::text NOT NULL,
    provenance text NOT NULL,
    source text DEFAULT ''::text NOT NULL,
    collector_node_id uuid,
    CONSTRAINT metric_series_owner_kind_check CHECK ((owner_kind = ANY (ARRAY['component'::text, 'system'::text, 'location'::text, 'node'::text]))),
    CONSTRAINT metric_series_owner_arc_check CHECK ((((owner_kind = 'component'::text) AND (component_id IS NOT NULL) AND (system_id IS NULL) AND (location_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'system'::text) AND (system_id IS NOT NULL) AND (component_id IS NULL) AND (location_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'location'::text) AND (location_id IS NOT NULL) AND (component_id IS NULL) AND (system_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'node'::text) AND (node_id IS NOT NULL) AND (component_id IS NULL) AND (system_id IS NULL) AND (location_id IS NULL)))),
    CONSTRAINT metric_series_provenance_check CHECK ((provenance = ANY (ARRAY['observed'::text, 'calculated'::text, 'intended'::text])))
);
-- The series key, leading with the owner so it is also the logical key's
-- index: every series of one owner, or of one owner's type, is a prefix read.
CREATE UNIQUE INDEX IF NOT EXISTS metric_series_key ON public.metric_series
    (owner_id, metric_type_id, instance, provenance, source, collector_node_id) NULLS NOT DISTINCT;
-- Every series of one type, across owners (an event rule's reconcile, a type's
-- retention window, a type's delete).
CREATE INDEX IF NOT EXISTS metric_series_type_idx ON public.metric_series (metric_type_id);

-- The hot tier: observed and calculated samples, partitioned by month on ts.
-- No primary key (a partitioned table's unique index must include ts, and
-- nothing references a sample) and no unique (series_id, ts): a poll cycle
-- stamps several samples in one instant (ADR-0061). No foreign key to the
-- series either: deleting an owner removes its series, and the samples left
-- behind are invisible to every reader (they all join the series) until their
-- month is dropped or retention reaches them.
CREATE TABLE IF NOT EXISTS public.metric (
    id bigint GENERATED ALWAYS AS IDENTITY,
    series_id bigint NOT NULL,
    ts timestamp with time zone DEFAULT now() NOT NULL,
    value double precision NOT NULL,
    source_rule_version bigint,
    source_rule text,
    source_rule_id uuid
) PARTITION BY RANGE (ts);
-- The one btree: a series' samples newest first, ties broken by id (the order
-- every latest-value read uses).
CREATE INDEX IF NOT EXISTS metric_series_ts_idx ON public.metric (series_id, ts DESC, id DESC);
-- Rows land in id order and, within a month, mostly in ts order, so a block
-- range index answers the event rule reconcile's id window and retention's ts
-- cutoff for a few bytes a page.
CREATE INDEX IF NOT EXISTS metric_id_brin ON public.metric USING brin (id);
CREATE INDEX IF NOT EXISTS metric_ts_brin ON public.metric USING brin (ts);

-- The declared tier: a command's intended rows, never pruned.
CREATE TABLE IF NOT EXISTS public.metric_declared (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    series_id bigint NOT NULL REFERENCES public.metric_series(id) ON DELETE CASCADE,
    ts timestamp with time zone DEFAULT now() NOT NULL,
    value double precision NOT NULL,
    event_id bigint REFERENCES public.event(id) ON DELETE SET NULL,
    command_id bigint NOT NULL REFERENCES public.command(id)
);
CREATE INDEX IF NOT EXISTS metric_declared_series_ts_idx ON public.metric_declared (series_id, ts DESC, id DESC);
CREATE INDEX IF NOT EXISTS metric_declared_command_idx ON public.metric_declared (command_id);

-- A partition per month the wide table holds a sample in, and this month and
-- the next two (the server keeps two ahead from here, and checks at boot).
-- Months are UTC.
DO $$
DECLARE
  m date;
BEGIN
  FOR m IN
    SELECT DISTINCT date_trunc('month', ts AT TIME ZONE 'UTC')::date FROM public.metric_wide WHERE provenance <> 'intended'
    UNION
    SELECT (date_trunc('month', now() AT TIME ZONE 'UTC') + make_interval(months => g))::date FROM generate_series(0, 2) AS g
  LOOP
    EXECUTE format('CREATE TABLE IF NOT EXISTS public.%I PARTITION OF public.metric FOR VALUES FROM (%L) TO (%L)',
      'metric_p' || to_char(m, 'YYYYMM'),
      to_char(m, 'YYYY-MM-DD') || ' 00:00:00+00',
      to_char(m + interval '1 month', 'YYYY-MM-DD') || ' 00:00:00+00');
  END LOOP;
END $$;

-- migrate:down
DROP TABLE IF EXISTS public.metric_declared;
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM pg_class WHERE oid = to_regclass('public.metric') AND relkind = 'p') THEN
    DROP TABLE public.metric;
  END IF;
END $$;
DROP TABLE IF EXISTS public.metric_series;
DO $$ BEGIN
  IF to_regclass('public.metric_wide') IS NOT NULL AND to_regclass('public.metric') IS NULL THEN
    ALTER TABLE public.metric_wide RENAME TO metric;
    ALTER INDEX IF EXISTS public.metric_wide_pkey RENAME TO metric_pkey;
    ALTER INDEX IF EXISTS public.metric_wide_owner_idx RENAME TO metric_owner_idx;
    ALTER INDEX IF EXISTS public.metric_wide_arc_owner_idx RENAME TO metric_arc_owner_idx;
    ALTER INDEX IF EXISTS public.metric_wide_ts_brin RENAME TO metric_ts_brin;
    ALTER SEQUENCE IF EXISTS public.metric_wide_id_seq RENAME TO metric_id_seq;
  END IF;
END $$;
