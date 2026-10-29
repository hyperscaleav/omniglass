-- migrate:up
-- #1275, second of three: the one-time copy of every wide metric row into the
-- narrow lane, in one transaction, refused unless every row arrived. One series
-- per distinct (type, owner, instance, provenance, source) the wide table
-- holds; then each observed or calculated row as a sample of its series, and
-- each intended row in the declared tier, every row keeping its id. A
-- collected series' collecting node was never recorded, so every series from
-- before this has none. The copy only runs into empty narrow tables, so a
-- repeat copies nothing; the count check then still holds.

INSERT INTO public.metric_series (metric_type_id, owner_kind, component_id, system_id, location_id, node_id, instance, provenance, source,
    source_conduit_id)
SELECT DISTINCT metric_type_id, owner_kind, component_id, system_id, location_id, node_id, instance, provenance, source, source_conduit_id
FROM public.metric_wide
ON CONFLICT DO NOTHING;

INSERT INTO public.metric (id, series_id, ts, value, source_rule_version, source_rule, source_rule_id)
OVERRIDING SYSTEM VALUE
SELECT w.id, s.id, w.ts, w.value, w.source_rule_version, w.source_rule, w.source_rule_id
FROM public.metric_wide w
JOIN public.metric_series s
  ON s.owner_id = COALESCE(w.component_id, w.system_id, w.location_id, w.node_id)
 AND s.metric_type_id = w.metric_type_id AND s.instance = w.instance
 AND s.provenance = w.provenance AND s.source = w.source AND s.collector_node_id IS NULL
 AND COALESCE(s.source_conduit_id, '00000000-0000-0000-0000-000000000000') = COALESCE(w.source_conduit_id, '00000000-0000-0000-0000-000000000000')
WHERE w.provenance IN ('observed', 'calculated')
  AND NOT EXISTS (SELECT 1 FROM public.metric LIMIT 1);

INSERT INTO public.metric_declared (id, series_id, ts, value, event_id, command_id)
OVERRIDING SYSTEM VALUE
SELECT w.id, s.id, w.ts, w.value, w.event_id, w.command_id
FROM public.metric_wide w
JOIN public.metric_series s
  ON s.owner_id = COALESCE(w.component_id, w.system_id, w.location_id, w.node_id)
 AND s.metric_type_id = w.metric_type_id AND s.instance = w.instance
 AND s.provenance = w.provenance AND s.source = w.source AND s.collector_node_id IS NULL
 AND COALESCE(s.source_conduit_id, '00000000-0000-0000-0000-000000000000') = COALESCE(w.source_conduit_id, '00000000-0000-0000-0000-000000000000')
WHERE w.provenance = 'intended'
  AND NOT EXISTS (SELECT 1 FROM public.metric_declared LIMIT 1);

-- Ids carry on from the wide table's: a sample's id is what the event rule
-- reconcile marks its place by, and a declared row's never meets a sample's.
SELECT setval(pg_get_serial_sequence('public.metric', 'id'),
  GREATEST((SELECT last_value FROM public.metric_wide_id_seq), (SELECT COALESCE(max(id), 0) FROM public.metric), 1));
SELECT setval(pg_get_serial_sequence('public.metric_declared', 'id'),
  GREATEST((SELECT last_value FROM public.metric_wide_id_seq), (SELECT COALESCE(max(id), 0) FROM public.metric_declared), 1));

DO $$
DECLARE
  wide bigint;
  narrow bigint;
BEGIN
  SELECT count(*) INTO wide FROM public.metric_wide;
  SELECT (SELECT count(*) FROM public.metric) + (SELECT count(*) FROM public.metric_declared) INTO narrow;
  IF wide <> narrow THEN
    RAISE EXCEPTION 'metric backfill: the wide table holds % rows and % were copied', wide, narrow;
  END IF;
END $$;

-- migrate:down
-- The copy back, into the wide table the next migration's down recreated
-- empty: each sample and declared row as one wide row, the series' columns
-- beside it. A declared row takes a fresh id, since the two narrow tables'
-- ids are only unique each within its own.
INSERT INTO public.metric_wide (id, ts, owner_kind, component_id, system_id, location_id, node_id, metric_type_id,
    instance, value, provenance, source, source_rule, source_rule_version, source_rule_id, source_conduit_id)
OVERRIDING SYSTEM VALUE
SELECT m.id, m.ts, s.owner_kind, s.component_id, s.system_id, s.location_id, s.node_id, s.metric_type_id,
    s.instance, m.value, s.provenance, s.source, m.source_rule, m.source_rule_version, m.source_rule_id, s.source_conduit_id
FROM public.metric m JOIN public.metric_series s ON s.id = m.series_id
WHERE NOT EXISTS (SELECT 1 FROM public.metric_wide LIMIT 1);
SELECT setval('public.metric_wide_id_seq',
  GREATEST((SELECT COALESCE(max(id), 0) FROM public.metric_wide), (SELECT last_value FROM public.metric_wide_id_seq), 1));
INSERT INTO public.metric_wide (ts, owner_kind, component_id, system_id, location_id, node_id, metric_type_id,
    instance, value, provenance, source, event_id, command_id, source_conduit_id)
SELECT d.ts, s.owner_kind, s.component_id, s.system_id, s.location_id, s.node_id, s.metric_type_id,
    s.instance, d.value, s.provenance, s.source, d.event_id, d.command_id, s.source_conduit_id
FROM public.metric_declared d JOIN public.metric_series s ON s.id = d.series_id
WHERE NOT EXISTS (SELECT 1 FROM public.metric_wide w WHERE w.provenance = 'intended');
DELETE FROM public.metric_declared;
TRUNCATE public.metric;
DELETE FROM public.metric_series;
