-- migrate:up
-- #1159: the event table's retention indexes. The retention pass prunes an
-- event past its origin's window, an origin at a time, unless something still
-- references it (the reference floor). Each prune searches exactly the rows it
-- may delete by (origin, ts), and each floor probe and each foreign key's action
-- a deleted event runs reaches an index of its own, so a chunk never scans the
-- event, command or sample tables. TestPruneEventsReachesItsIndexes explains
-- each and fails if one stops reaching its index.
--
-- The references into event, all partial on a link that exists since most rows
-- have none:
--   command.caused_event_id, command.settled_event_id: no on delete action, so
--     Postgres checks the command table for each deleted event (and the floor
--     probes them first);
--   event.source_event_id: a derived event's source, the same;
--   property.event_id, metric_declared.event_id: on delete set null, an update
--     by that column per deleted event.
-- Deliberately NOT concurrently, for the reasons
-- 20260813100000_property_system_owner_index.sql gives (dbmate's transaction,
-- and no INVALID index left behind on a failed build).
CREATE INDEX IF NOT EXISTS event_origin_ts_idx
    ON public.event USING btree (origin, ts);
CREATE INDEX IF NOT EXISTS event_source_event_idx
    ON public.event USING btree (source_event_id) WHERE (source_event_id IS NOT NULL);
CREATE INDEX IF NOT EXISTS command_caused_event_idx
    ON public.command USING btree (caused_event_id) WHERE (caused_event_id IS NOT NULL);
CREATE INDEX IF NOT EXISTS command_settled_event_idx
    ON public.command USING btree (settled_event_id) WHERE (settled_event_id IS NOT NULL);
CREATE INDEX IF NOT EXISTS property_event_idx
    ON public.property USING btree (event_id) WHERE (event_id IS NOT NULL);
CREATE INDEX IF NOT EXISTS metric_declared_event_idx
    ON public.metric_declared USING btree (event_id) WHERE (event_id IS NOT NULL);

-- migrate:down
DROP INDEX IF EXISTS public.metric_declared_event_idx;
DROP INDEX IF EXISTS public.property_event_idx;
DROP INDEX IF EXISTS public.command_settled_event_idx;
DROP INDEX IF EXISTS public.command_caused_event_idx;
DROP INDEX IF EXISTS public.event_source_event_idx;
DROP INDEX IF EXISTS public.event_origin_ts_idx;
